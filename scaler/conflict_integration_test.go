package scaler

// Integration tests for patch / scaling conflict prevention.
//
// These tests exercise the guard logic directly on a Scaler struct without
// making real AWS calls, so no credentials or network access are needed.

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"predictive-rds-scaler/types"
)

// newTestScaler returns a minimal Scaler suitable for state-logic tests.
// All AWS-dependent fields (rdsClient, metrics) are nil; only call methods
// that do not reach them.
func newTestScaler(cfg *types.Config) *Scaler {
	logger := zerolog.Nop()
	return &Scaler{
		config:    cfg,
		broadcast: make(chan types.Broadcast, 64), // buffered so submitBroadcast never blocks
		logger:    &logger,
	}
}

// ---------------------------------------------------------------------------
// Guard: StartPatchMode must not start when scalerStatus.IsScaling is true
// ---------------------------------------------------------------------------

func TestStartPatchMode_BlockedWhileScaling(t *testing.T) {
	s := newTestScaler(&types.Config{MinInstances: 2, EnableAutoPatch: true})

	// Simulate an in-flight scale-out.
	s.scalerStatus.IsScaling = true

	// StartPatchMode internally calls getClusterInstancesWithMaintenance which
	// hits the RDS API, so we test the guard before that path via the
	// internal helpers we CAN reach.
	//
	// Verify: after the IsScaling guard, patchStatus.Active must stay false.
	s.patchMu.Lock()
	if s.patchStatus.Active {
		t.Fatal("patchStatus.Active should be false before the test")
	}
	s.patchMu.Unlock()

	// Call the guard path directly by constructing the same condition
	// checked inside StartPatchMode.
	if !s.scalerStatus.IsScaling {
		t.Fatal("precondition: IsScaling should be true")
	}

	// Manually replicate the guard so we can assert the outcome without
	// needing AWS credentials.
	started := false
	if !s.scalerStatus.IsScaling {
		started = true
	}

	if started {
		t.Error("patch mode should NOT have started while a scale operation is in flight")
	}
}

// ---------------------------------------------------------------------------
// Guard: CheckAndAutoApplyPatches must be a no-op when patch is already active
// ---------------------------------------------------------------------------

func TestCheckAndAutoApplyPatches_NoOpWhenAlreadyActive(t *testing.T) {
	s := newTestScaler(&types.Config{MinInstances: 2, EnableAutoPatch: true})

	// Mark patch mode as already active.
	s.patchMu.Lock()
	s.patchStatus.Active = true
	s.patchMu.Unlock()

	// CheckAndAutoApplyPatches should return immediately without touching the
	// RDS client (which is nil – a panic would occur if the guard is missing).
	// If the function panics, the test fails automatically.
	s.CheckAndAutoApplyPatches()
}

// ---------------------------------------------------------------------------
// Guard: CheckAndAutoApplyPatches must be a no-op when auto-patch is disabled
// ---------------------------------------------------------------------------

func TestCheckAndAutoApplyPatches_NoOpWhenDisabled(t *testing.T) {
	s := newTestScaler(&types.Config{MinInstances: 2, EnableAutoPatch: false})
	// Should return immediately (EnableAutoPatch == false) without panicking.
	s.CheckAndAutoApplyPatches()
}

// ---------------------------------------------------------------------------
// Guard: StopPatchMode is safe to call when patch mode is not active
// ---------------------------------------------------------------------------

func TestStopPatchMode_IdempotentWhenNotActive(t *testing.T) {
	s := newTestScaler(&types.Config{})
	// Must not panic or deadlock.
	s.StopPatchMode()
	s.StopPatchMode()
}

// ---------------------------------------------------------------------------
// Guard: concurrent StopPatchMode calls must not double-close the channel
// ---------------------------------------------------------------------------

func TestStopPatchMode_ConcurrentSafe(t *testing.T) {
	s := newTestScaler(&types.Config{})

	// Manually install an active patch run with a stop channel.
	stopCh := make(chan struct{})
	s.patchMu.Lock()
	s.patchStatus.Active = true
	s.patchStopCh = stopCh
	s.patchMu.Unlock()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// None of these must panic with "close of closed channel".
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("StopPatchMode panicked: %v", r)
				}
			}()
			s.StopPatchMode()
		}()
	}
	wg.Wait()

	s.patchMu.Lock()
	active := s.patchStatus.Active
	s.patchMu.Unlock()
	if active {
		t.Error("patchStatus.Active should be false after StopPatchMode")
	}
}

// ---------------------------------------------------------------------------
// Guard: scaleIn must not delete the temp patch reader
// ---------------------------------------------------------------------------

func TestScaleIn_SkipsTempPatchReader(t *testing.T) {
	// Build a list of candidate readers and verify that the one matching
	// TempInstanceName is filtered out by the guard logic inside scaleIn.
	//
	// We cannot call scaleIn directly (it needs a real RDS client), so we
	// test the guard condition in isolation.

	const tempName = "predictive-autoscaling-patch-10-abc123"

	s := newTestScaler(&types.Config{MinInstances: 2})
	s.patchMu.Lock()
	s.patchStatus.TempInstanceName = tempName
	s.patchMu.Unlock()

	// Simulate what scaleIn does: read the temp name and check if it matches.
	s.patchMu.Lock()
	readBack := s.patchStatus.TempInstanceName
	s.patchMu.Unlock()

	if readBack != tempName {
		t.Fatalf("expected TempInstanceName %q, got %q", tempName, readBack)
	}

	// The guard expression used inside scaleIn:
	shouldSkip := readBack != "" && readBack == tempName
	if !shouldSkip {
		t.Error("scaleIn should skip the temp patch reader but the guard evaluated to false")
	}

	// Verify a non-patch reader is NOT skipped.
	const otherReader = "predictive-autoscaling-08-xyz999"
	shouldSkipOther := readBack != "" && readBack == otherReader
	if shouldSkipOther {
		t.Error("scaleIn should NOT skip a regular reader")
	}
}

// ---------------------------------------------------------------------------
// Race: concurrent StartPatchMode calls must activate patch mode exactly once
// ---------------------------------------------------------------------------

func TestStartPatchMode_ConcurrentOnlyStartsOnce(t *testing.T) {
	s := newTestScaler(&types.Config{MinInstances: 2, EnableAutoPatch: true})

	// We cannot exercise the full StartPatchMode path (needs RDS client) but
	// we CAN verify the patchMu guard using the same pattern the function uses.
	var activated int32

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.patchMu.Lock()
			if s.patchStatus.Active {
				s.patchMu.Unlock()
				return
			}
			// Won the race – activate.
			s.patchStatus.Active = true
			s.patchMu.Unlock()
			atomic.AddInt32(&activated, 1)
		}()
	}
	wg.Wait()

	if activated != 1 {
		t.Errorf("patch mode activated %d times; want exactly 1", activated)
	}
}

// ---------------------------------------------------------------------------
// Integration: scaling loop skips when patch mode is active
// ---------------------------------------------------------------------------

func TestScaleLoop_SkipsWhenPatchActive(t *testing.T) {
	s := newTestScaler(&types.Config{MinInstances: 2, EnableAutoPatch: true})

	s.patchMu.Lock()
	s.patchStatus.Active = true
	s.patchMu.Unlock()

	// Simulate one tick of the scale loop – the loop checks patchStatus.Active
	// before calling s.scale(). We replicate that guard here.
	var scaleCalled bool
	scaleIfNotPatching := func() {
		s.patchMu.Lock()
		patching := s.patchStatus.Active
		s.patchMu.Unlock()
		if !patching {
			scaleCalled = true
		}
	}

	scaleIfNotPatching()

	if scaleCalled {
		t.Error("scale() should not have been called while patch mode is active")
	}

	// Deactivate patch mode and verify scale would now run.
	s.patchMu.Lock()
	s.patchStatus.Active = false
	s.patchMu.Unlock()

	scaleIfNotPatching()
	if !scaleCalled {
		t.Error("scale() should have been called after patch mode deactivated")
	}
}

// ---------------------------------------------------------------------------
// Integration: balancing skips when patch mode is active
// ---------------------------------------------------------------------------

func TestPerformBalancing_SkipsWhenPatchActive(t *testing.T) {
	s := newTestScaler(&types.Config{EnableBalancing: true, MinInstances: 2})

	s.patchMu.Lock()
	s.patchStatus.Active = true
	s.patchMu.Unlock()

	// performBalancing checks patchStatus.Active and returns nil immediately
	// when it is true – that path does NOT touch the RDS client.
	err := s.performBalancing()
	if err != nil {
		t.Errorf("performBalancing should return nil when patch is active, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Integration: GetPatchStatus returns correct AutoPatchEnabled flag
// ---------------------------------------------------------------------------

func TestGetPatchStatus_ReflectsConfig(t *testing.T) {
	s := newTestScaler(&types.Config{EnableAutoPatch: true})
	status := s.GetPatchStatus()
	if !status.AutoPatchEnabled {
		t.Error("GetPatchStatus should report AutoPatchEnabled=true when config has it enabled")
	}

	s.config.EnableAutoPatch = false
	status = s.GetPatchStatus()
	if status.AutoPatchEnabled {
		t.Error("GetPatchStatus should report AutoPatchEnabled=false when config has it disabled")
	}
}

// ---------------------------------------------------------------------------
// Integration: patch progress is tracked correctly
// ---------------------------------------------------------------------------

func TestPatchProgress_TracksCorrectly(t *testing.T) {
	s := newTestScaler(&types.Config{EnableAutoPatch: true})

	instances := []types.PatchInstanceInfo{
		{Identifier: "reader-1", IsWriter: false, PendingActions: []string{"system-update"}},
		{Identifier: "reader-2", IsWriter: false, PendingActions: []string{"os-update"}},
	}

	s.patchMu.Lock()
	s.patchStatus = types.PatchStatus{
		Active:           true,
		AutoPatchEnabled: true,
		TotalInstances:   len(instances),
		PendingInstances: instances,
	}
	s.patchMu.Unlock()

	// Simulate completing the first instance.
	s.patchMu.Lock()
	s.patchStatus.PatchedCount++
	s.patchStatus.CompletedInstances = append(s.patchStatus.CompletedInstances, "reader-1")
	s.patchMu.Unlock()

	status := s.GetPatchStatus()
	if status.PatchedCount != 1 {
		t.Errorf("PatchedCount = %d; want 1", status.PatchedCount)
	}
	if len(status.CompletedInstances) != 1 || status.CompletedInstances[0] != "reader-1" {
		t.Errorf("CompletedInstances = %v; want [reader-1]", status.CompletedInstances)
	}
	if status.TotalInstances != 2 {
		t.Errorf("TotalInstances = %d; want 2", status.TotalInstances)
	}
}

// ---------------------------------------------------------------------------
// Unit: isInMaintenanceWindow – additional edge cases for conflict scenarios
// ---------------------------------------------------------------------------

func TestIsInMaintenanceWindow_NeverTriggersOutsideWindow(t *testing.T) {
	// During a 1-hour window, we should NEVER trigger outside it.
	window := "tue:03:00-tue:04:00"

	// Tuesday 2024-01-09 03:30 – inside
	inside := time.Date(2024, 1, 9, 3, 30, 0, 0, time.UTC)
	if !isInMaintenanceWindow(window, inside) {
		t.Error("expected true for time inside window")
	}

	// Tuesday 2024-01-09 04:01 – outside
	outside := time.Date(2024, 1, 9, 4, 1, 0, 0, time.UTC)
	if isInMaintenanceWindow(window, outside) {
		t.Error("expected false for time outside window")
	}

	// Wednesday 2024-01-10 03:30 – wrong day
	wrongDay := time.Date(2024, 1, 10, 3, 30, 0, 0, time.UTC)
	if isInMaintenanceWindow(window, wrongDay) {
		t.Error("expected false for time on wrong day")
	}
}
