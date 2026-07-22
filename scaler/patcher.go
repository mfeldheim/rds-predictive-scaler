package scaler

import (
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/rds"
	"predictive-rds-scaler/types"
)

// getPendingMaintenanceActions returns a map of instanceARN → list of pending action types
// for all instances in the cluster.
func (s *Scaler) getPendingMaintenanceActions() (map[string][]string, error) {
	input := &rds.DescribePendingMaintenanceActionsInput{}
	result, err := s.rdsClient.DescribePendingMaintenanceActions(input)
	if err != nil {
		return nil, fmt.Errorf("failed to describe pending maintenance actions: %v", err)
	}

	pending := make(map[string][]string)
	for _, resource := range result.PendingMaintenanceActions {
		arn := aws.StringValue(resource.ResourceIdentifier)
		for _, action := range resource.PendingMaintenanceActionDetails {
			pending[arn] = append(pending[arn], aws.StringValue(action.Action))
		}
	}
	return pending, nil
}

// getClusterInstancesWithMaintenance returns PatchInstanceInfo for all cluster instances,
// enriched with maintenance window and pending action data.
func (s *Scaler) getClusterInstancesWithMaintenance() ([]types.PatchInstanceInfo, error) {
	pendingByArn, err := s.getPendingMaintenanceActions()
	if err != nil {
		return nil, err
	}

	allInstances, err := s.getAllInstances()
	if err != nil {
		return nil, err
	}

	writerInstance, err := s.getWriterInstance()
	if err != nil {
		return nil, err
	}
	writerID := aws.StringValue(writerInstance.DBInstanceIdentifier)

	var infos []types.PatchInstanceInfo
	for _, inst := range allInstances {
		id := aws.StringValue(inst.DBInstanceIdentifier)
		arn := aws.StringValue(inst.DBInstanceArn)
		window := aws.StringValue(inst.PreferredMaintenanceWindow)
		actions := pendingByArn[arn]

		infos = append(infos, types.PatchInstanceInfo{
			Identifier:        id,
			IsWriter:          id == writerID,
			MaintenanceWindow: window,
			PendingActions:    actions,
		})
	}
	return infos, nil
}

// isInMaintenanceWindow checks whether the given UTC time falls within an AWS maintenance window
// string of the form "ddd:hh24:mi-ddd:hh24:mi" (e.g. "mon:05:00-mon:06:00").
func isInMaintenanceWindow(window string, t time.Time) bool {
	parts := strings.SplitN(window, "-", 2)
	if len(parts) != 2 {
		return false
	}
	start, err1 := parseWindowTime(parts[0], t)
	end, err2 := parseWindowTime(parts[1], t)
	if err1 != nil || err2 != nil {
		return false
	}

	// Handle window wrapping across week boundary
	if end.Before(start) {
		end = end.Add(7 * 24 * time.Hour)
	}

	// Check if t falls in [start, end) within this week or the previous week
	weekAgo := t.Add(-7 * 24 * time.Hour)
	startPrev, _ := parseWindowTime(parts[0], weekAgo)
	endPrev, _ := parseWindowTime(parts[1], weekAgo)
	if endPrev.Before(startPrev) {
		endPrev = endPrev.Add(7 * 24 * time.Hour)
	}

	return (t.Equal(start) || t.After(start)) && t.Before(end) ||
		(t.Equal(startPrev) || t.After(startPrev)) && t.Before(endPrev)
}

// parseWindowTime parses "ddd:hh:mm" relative to the given reference time's week.
func parseWindowTime(s string, ref time.Time) (time.Time, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) != 3 {
		return time.Time{}, fmt.Errorf("invalid window time: %s", s)
	}

	dayMap := map[string]time.Weekday{
		"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday,
		"wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday,
		"sat": time.Saturday,
	}
	day, ok := dayMap[strings.ToLower(parts[0])]
	if !ok {
		return time.Time{}, fmt.Errorf("invalid day: %s", parts[0])
	}

	var hour, min int
	if _, err := fmt.Sscanf(parts[1], "%d", &hour); err != nil {
		return time.Time{}, err
	}
	if _, err := fmt.Sscanf(parts[2], "%d", &min); err != nil {
		return time.Time{}, err
	}

	// Anchor to the Sunday of the ref week
	ref = ref.UTC()
	weekStart := ref.AddDate(0, 0, -int(ref.Weekday()))
	weekStart = time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day(), 0, 0, 0, 0, time.UTC)

	return weekStart.Add(time.Duration(day) * 24 * time.Hour).Add(time.Duration(hour) * time.Hour).Add(time.Duration(min) * time.Minute), nil
}

// isClusterHealthy returns true when enough instances are available and no scaling is in progress.
func (s *Scaler) isClusterHealthy() bool {
	if s.scalerStatus.IsScaling {
		return false
	}
	clusterStatus, err := s.getClusterStatus()
	if err != nil {
		return false
	}
	return clusterStatus.CurrentActiveReaders >= s.config.MinInstances
}

// CheckAndAutoApplyPatches is called periodically when auto-patch is enabled.
// It finds instances with pending maintenance whose window is currently active
// and triggers patch mode for them.
func (s *Scaler) CheckAndAutoApplyPatches() {
	if !s.config.EnableAutoPatch {
		return
	}

	s.patchMu.Lock()
	if s.patchStatus.Active {
		s.patchMu.Unlock()
		return
	}
	s.patchMu.Unlock()

	infos, err := s.getClusterInstancesWithMaintenance()
	if err != nil {
		s.logger.Error().Err(err).Msg("Auto-patch: failed to get instance maintenance info")
		return
	}

	now := time.Now().UTC()
	var toApply []types.PatchInstanceInfo
	for _, info := range infos {
		if len(info.PendingActions) == 0 {
			continue
		}
		if !isInMaintenanceWindow(info.MaintenanceWindow, now) {
			s.logger.Debug().
				Str("instance", info.Identifier).
				Str("window", info.MaintenanceWindow).
				Msg("Auto-patch: pending maintenance but not in window yet")
			continue
		}
		s.logger.Info().
			Str("instance", info.Identifier).
			Strs("actions", info.PendingActions).
			Str("window", info.MaintenanceWindow).
			Msg("Auto-patch: instance is in maintenance window with pending actions")
		toApply = append(toApply, info)
	}

	if len(toApply) == 0 {
		return
	}

	// Don't start while a scale operation is in flight.
	if s.scalerStatus.IsScaling {
		s.logger.Warn().Msg("Auto-patch: scaling operation in progress, deferring patch")
		return
	}

	if !s.isClusterHealthy() {
		s.logger.Warn().Msg("Auto-patch: cluster not healthy, deferring patch")
		return
	}

	s.logger.Info().Int("count", len(toApply)).Msg("Auto-patch: starting patch mode")
	s.startPatchModeForInstances(toApply)
}

// StartPatchMode triggers an immediate rolling patch of all instances with pending maintenance,
// regardless of maintenance window.
func (s *Scaler) StartPatchMode() {
	s.patchMu.Lock()
	if s.patchStatus.Active {
		s.patchMu.Unlock()
		s.logger.Warn().Msg("Patch mode already active")
		return
	}
	s.patchMu.Unlock()

	// Don't start while a scale operation (or another patch step) is in flight.
	if s.scalerStatus.IsScaling {
		s.logger.Warn().Msg("StartPatchMode: scaling operation in progress, deferring")
		return
	}

	infos, err := s.getClusterInstancesWithMaintenance()
	if err != nil {
		s.logger.Error().Err(err).Msg("StartPatchMode: failed to get maintenance info")
		return
	}

	var toApply []types.PatchInstanceInfo
	for _, info := range infos {
		if len(info.PendingActions) > 0 {
			toApply = append(toApply, info)
		}
	}

	if len(toApply) == 0 {
		s.logger.Info().Msg("StartPatchMode: no instances with pending maintenance")
		return
	}

	s.startPatchModeForInstances(toApply)
}

// StopPatchMode cancels an in-progress patch run.
func (s *Scaler) StopPatchMode() {
	s.patchMu.Lock()
	if s.patchStopCh != nil {
		close(s.patchStopCh)
		s.patchStopCh = nil
	}
	s.patchStatus.Active = false
	s.patchStatus.CurrentlyPatching = ""
	s.patchStatus.TempInstanceName = ""
	status := s.patchStatus
	s.patchMu.Unlock()

	s.submitBroadcast(&types.Broadcast{MessageType: "patchStatus", Data: status})
	s.logger.Info().Msg("Patch mode stopped")
}

func (s *Scaler) startPatchModeForInstances(instances []types.PatchInstanceInfo) {
	s.patchMu.Lock()
	s.patchStopCh = make(chan struct{})
	s.patchStatus = types.PatchStatus{
		Active:           true,
		AutoPatchEnabled: s.config.EnableAutoPatch,
		TotalInstances:   len(instances),
		PendingInstances: instances,
	}
	stopCh := s.patchStopCh
	s.patchMu.Unlock()

	s.broadcastPatchStatus()
	go s.runPatchMode(instances, stopCh)
}

// runPatchMode performs a rolling patch of the given instances.
// Readers are patched first; the writer is patched last via failover.
func (s *Scaler) runPatchMode(instances []types.PatchInstanceInfo, stopCh chan struct{}) {
	defer func() {
		s.patchMu.Lock()
		s.patchStatus.Active = false
		s.patchStatus.CurrentlyPatching = ""
		s.patchStatus.TempInstanceName = ""
		if s.patchStopCh == stopCh {
			s.patchStopCh = nil
		}
		s.patchMu.Unlock()
		s.broadcastPatchStatus()
		
		// Signal to resume scaling immediately
		select {
		case s.patchDone <- struct{}{}:
		default:
		}
		
		s.logger.Info().Msg("Patch mode completed")
	}()

	// Patch readers first, writer last
	var writerInfo *types.PatchInstanceInfo
	var readers []types.PatchInstanceInfo
	for i := range instances {
		if instances[i].IsWriter {
			writerInfo = &instances[i]
		} else {
			readers = append(readers, instances[i])
		}
	}
	ordered := append(readers, func() []types.PatchInstanceInfo {
		if writerInfo != nil {
			return []types.PatchInstanceInfo{*writerInfo}
		}
		return nil
	}()...)

	for _, info := range ordered {
		select {
		case <-stopCh:
			s.logger.Info().Msg("Patch mode cancelled")
			return
		default:
		}

		if err := s.patchInstance(info, stopCh); err != nil {
			s.logger.Error().Err(err).Str("instance", info.Identifier).Msg("Patch mode: error patching instance, aborting")
			return
		}

		s.patchMu.Lock()
		s.patchStatus.PatchedCount++
		s.patchStatus.CompletedInstances = append(s.patchStatus.CompletedInstances, info.Identifier)
		s.patchMu.Unlock()
		s.broadcastPatchStatus()
	}
}

// patchInstance handles the full rolling patch of a single instance:
//  1. Start a temporary +1 reader so the cluster stays at required+1 during patching
//  2. If the instance is the writer, fail over to the temp reader first
//  3. Apply pending maintenance to the target instance
//  4. Wait for the target to return to "available"
//  5. Remove the temporary reader
func (s *Scaler) patchInstance(info types.PatchInstanceInfo, stopCh chan struct{}) error {
	s.patchMu.Lock()
	s.patchStatus.CurrentlyPatching = info.Identifier
	s.patchMu.Unlock()
	s.broadcastPatchStatus()

	s.logger.Info().
		Str("instance", info.Identifier).
		Bool("isWriter", info.IsWriter).
		Strs("actions", info.PendingActions).
		Msg("Patch mode: patching instance")

	// --- Step 1: add temporary reader ---
	tempName, err := s.addTempReader()
	if err != nil {
		return fmt.Errorf("failed to add temporary reader: %v", err)
	}

	s.patchMu.Lock()
	s.patchStatus.TempInstanceName = tempName
	s.patchMu.Unlock()
	s.broadcastPatchStatus()

	s.logger.Info().Str("temp", tempName).Msg("Patch mode: waiting for temp reader to become available")
	if err := s.waitForInstancesAvailable([]string{tempName}); err != nil {
		_ = s.deleteTempReader(tempName)
		return fmt.Errorf("temp reader never became available: %v", err)
	}

	// --- Step 2: failover if this is the writer ---
	if info.IsWriter {
		s.logger.Info().Str("target", tempName).Msg("Patch mode: failing over to temp reader before patching writer")
		if err := s.failoverToInstance(tempName); err != nil {
			_ = s.deleteTempReader(tempName)
			return fmt.Errorf("failover failed: %v", err)
		}
		// After failover the old writer is now a reader – update info so we patch it as a reader
		s.logger.Info().Str("former_writer", info.Identifier).Msg("Patch mode: failover done, former writer is now a reader")
	}

	// --- Step 3: apply pending maintenance on target ---
	select {
	case <-stopCh:
		_ = s.deleteTempReader(tempName)
		return fmt.Errorf("cancelled")
	default:
	}

	if err := s.applyPendingMaintenance(info.Identifier, info.PendingActions); err != nil {
		_ = s.deleteTempReader(tempName)
		return fmt.Errorf("failed to apply maintenance: %v", err)
	}

	// --- Step 4: wait for target to become available again ---
	s.logger.Info().Str("instance", info.Identifier).Msg("Patch mode: waiting for patched instance to become available")
	if err := s.waitForInstancesAvailable([]string{info.Identifier}); err != nil {
		_ = s.deleteTempReader(tempName)
		return fmt.Errorf("patched instance never became available: %v", err)
	}

	// --- Step 4b: if we patched the writer, failover back before deleting temp ---
	if info.IsWriter {
		s.logger.Info().Str("target", info.Identifier).Msg("Patch mode: failing back to patched writer instance")
		if err := s.failoverToInstance(info.Identifier); err != nil {
			s.logger.Warn().Err(err).Str("instance", info.Identifier).Msg("Patch mode: failover back to writer failed, proceeding with temp reader cleanup")
		} else {
			s.logger.Info().Msg("Patch mode: failover back to writer completed successfully")
		}
	}

	// --- Step 5: remove temp reader ---
	if err := s.deleteTempReader(tempName); err != nil {
		s.logger.Warn().Err(err).Str("temp", tempName).Msg("Patch mode: failed to delete temp reader (will be cleaned up by auto-scaler)")
	}

	s.patchMu.Lock()
	s.patchStatus.TempInstanceName = ""
	s.patchMu.Unlock()

	s.logger.Info().Str("instance", info.Identifier).Msg("Patch mode: instance patched successfully")
	return nil
}

// addTempReader creates a new reader instance and returns its name.
func (s *Scaler) addTempReader() (string, error) {
	writerInstance, err := s.getWriterInstance()
	if err != nil {
		return "", fmt.Errorf("failed to get writer instance: %v", err)
	}

	readerInstancePool, _ := parseReaderInstanceClasses(s.config.ReaderInstanceClasses)
	readers, err := s.getReaderInstances(StatusAll ^ StatusDeleting)
	if err != nil {
		return "", err
	}
	selectedClass := s.selectInstanceClass(readerInstancePool, readers)

	currentHour := time.Now().UTC().Hour()
	name := fmt.Sprintf("%spatch-%d-%s", s.config.InstanceNamePrefix, currentHour, generateRandomUID())

	_, err = s.createReaderInstance(name, writerInstance, selectedClass)
	if err != nil {
		return "", fmt.Errorf("failed to create temp reader: %v", err)
	}
	return name, nil
}

// deleteTempReader removes a temporary reader instance.
func (s *Scaler) deleteTempReader(name string) error {
	_, err := s.rdsClient.DeleteDBInstance(&rds.DeleteDBInstanceInput{
		DBInstanceIdentifier: aws.String(name),
		SkipFinalSnapshot:    aws.Bool(true),
	})
	if err != nil {
		return fmt.Errorf("failed to delete temp reader %s: %v", name, err)
	}
	s.logger.Info().Str("temp", name).Msg("Patch mode: temp reader deletion initiated")
	return nil
}

// applyPendingMaintenance calls ApplyPendingMaintenanceAction for each pending action on the instance.
func (s *Scaler) applyPendingMaintenance(instanceID string, actions []string) error {
	// Resolve the instance ARN first
	desc, err := s.rdsClient.DescribeDBInstances(&rds.DescribeDBInstancesInput{
		DBInstanceIdentifier: aws.String(instanceID),
	})
	if err != nil || len(desc.DBInstances) == 0 {
		return fmt.Errorf("could not describe instance %s: %v", instanceID, err)
	}
	arn := aws.StringValue(desc.DBInstances[0].DBInstanceArn)

	for _, action := range actions {
		s.logger.Info().Str("instance", instanceID).Str("action", action).Msg("Patch mode: applying pending maintenance action")
		_, err := s.rdsClient.ApplyPendingMaintenanceAction(&rds.ApplyPendingMaintenanceActionInput{
			ResourceIdentifier: aws.String(arn),
			ApplyAction:        aws.String(action),
			OptInType:          aws.String("immediate"),
		})
		if err != nil {
			return fmt.Errorf("failed to apply action %s on %s: %v", action, instanceID, err)
		}
	}
	return nil
}

// broadcastPatchStatus sends the current patch status to all WebSocket clients.
func (s *Scaler) broadcastPatchStatus() {
	s.patchMu.Lock()
	status := s.patchStatus
	s.patchMu.Unlock()
	s.submitBroadcast(&types.Broadcast{MessageType: "patchStatus", Data: status})
}

// GetPatchStatus returns a copy of the current patch status (safe to call from outside).
func (s *Scaler) GetPatchStatus() types.PatchStatus {
	s.patchMu.Lock()
	defer s.patchMu.Unlock()
	status := s.patchStatus
	status.AutoPatchEnabled = s.config.EnableAutoPatch
	return status
}
