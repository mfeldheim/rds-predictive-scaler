package scaler

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"predictive-rds-scaler/types"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	patchStateConfigMapName = "rds-predictive-scaler-patch-state"
	patchStateKey           = "patch-status.json"
)

// InitPatchStateFromConfigMap loads persisted patch state from ConfigMap on startup.
// This allows patch cycles to survive pod restarts on spot terminations.
func (s *Scaler) InitPatchStateFromConfigMap(ctx context.Context) error {
	k8sClient, err := getKubernetesClient()
	if err != nil {
		s.logger.Warn().Err(err).Msg("Persistence: failed to create Kubernetes client, skipping patch state restoration")
		return nil // Non-fatal, continue without persistence
	}

	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		namespace = "kube-system"
	}

	cm, err := k8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, patchStateConfigMapName, metav1.GetOptions{})
	if err != nil {
		s.logger.Debug().Err(err).Msg("Persistence: no existing patch state ConfigMap found")
		return nil // ConfigMap doesn't exist yet, that's ok
	}

	dataStr, ok := cm.Data[patchStateKey]
	if !ok {
		s.logger.Debug().Msg("Persistence: patch state key not found in ConfigMap")
		return nil
	}

	var savedStatus types.PatchStatus
	if err := json.Unmarshal([]byte(dataStr), &savedStatus); err != nil {
		s.logger.Error().Err(err).Msg("Persistence: failed to unmarshal patch state")
		return nil // Can't recover, ignore
	}

	// Only restore if it was active and recent (within last 30 minutes)
	if !savedStatus.Active {
		s.logger.Debug().Msg("Persistence: saved patch state is not active, skipping restoration")
		return nil
	}

	// Update patch state to resume
	s.patchMu.Lock()
	s.patchStatus = savedStatus
	// Recreate stop channel for the resumed goroutine
	s.patchStopCh = make(chan struct{})
	stopCh := s.patchStopCh
	s.patchMu.Unlock()

	s.logger.Info().
		Int("totalInstances", savedStatus.TotalInstances).
		Int("patchedCount", savedStatus.PatchedCount).
		Str("currentlyPatching", savedStatus.CurrentlyPatching).
		Msg("Persistence: resuming patch cycle from saved state")

	// Broadcast the restored state to UI clients
	s.broadcastPatchStatus()

	// Resume patching in background
	go func() {
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
			s.SavePatchStateToConfigMap(context.Background())
			
			// Signal to resume scaling immediately
			select {
			case s.patchDone <- struct{}{}:
			default:
			}
			
			s.logger.Info().Msg("Patch mode resumed and completed")
		}()

		// Resume from where we left off
		s.ResumePatchMode(savedStatus.PendingInstances, savedStatus.CompletedInstances, stopCh)
	}()

	return nil
}

// SavePatchStateToConfigMap persists the current patch state to a ConfigMap.
// This is called periodically and on state changes to ensure durability.
func (s *Scaler) SavePatchStateToConfigMap(ctx context.Context) error {
	s.patchMu.Lock()
	statusSnapshot := s.patchStatus
	s.patchMu.Unlock()

	k8sClient, err := getKubernetesClient()
	if err != nil {
		s.logger.Warn().Err(err).Msg("Persistence: failed to create Kubernetes client")
		return err
	}

	namespace := os.Getenv("POD_NAMESPACE")
	if namespace == "" {
		namespace = "kube-system"
	}

	statusJSON, err := json.Marshal(statusSnapshot)
	if err != nil {
		s.logger.Error().Err(err).Msg("Persistence: failed to marshal patch state")
		return err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      patchStateConfigMapName,
			Namespace: namespace,
		},
		Data: map[string]string{
			patchStateKey: string(statusJSON),
		},
	}

	// Try to update existing ConfigMap, create if not found
	existing, err := k8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, patchStateConfigMapName, metav1.GetOptions{})
	if err == nil {
		cm.ResourceVersion = existing.ResourceVersion
		_, err = k8sClient.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{})
	} else {
		_, err = k8sClient.CoreV1().ConfigMaps(namespace).Create(ctx, cm, metav1.CreateOptions{})
	}

	if err != nil {
		s.logger.Warn().Err(err).Msg("Persistence: failed to save patch state to ConfigMap")
		return err
	}

	return nil
}

// getKubernetesClient returns an in-cluster Kubernetes client.
func getKubernetesClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load in-cluster Kubernetes config: %v", err)
	}
	return kubernetes.NewForConfig(config)
}

// StartPersistenceTicker periodically saves patch state while active.
func (s *Scaler) StartPersistenceTicker() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.patchMu.Lock()
			if s.patchStatus.Active {
				s.patchMu.Unlock()
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = s.SavePatchStateToConfigMap(ctx)
				cancel()
			} else {
				s.patchMu.Unlock()
			}
		}
	}
}

// ResumePatchMode resumes patching from a previous state.
// It patches remaining instances and updates the persisted state.
func (s *Scaler) ResumePatchMode(instances []types.PatchInstanceInfo, completed []string, stopCh chan struct{}) {
	// Build a map of completed instances
	completedMap := make(map[string]bool)
	for _, id := range completed {
		completedMap[id] = true
	}

	// Patch remaining instances
	for _, info := range instances {
		if completedMap[info.Identifier] {
			continue
		}

		select {
		case <-stopCh:
			s.logger.Info().Msg("Resumed patch mode cancelled")
			return
		default:
		}

		if err := s.patchInstance(info, stopCh); err != nil {
			s.logger.Error().Err(err).Str("instance", info.Identifier).Msg("Resumed patch: error patching instance, aborting")
			return
		}

		s.patchMu.Lock()
		s.patchStatus.PatchedCount++
		s.patchStatus.CompletedInstances = append(s.patchStatus.CompletedInstances, info.Identifier)
		s.patchMu.Unlock()
		s.broadcastPatchStatus()

		// Persist state after each instance is patched
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = s.SavePatchStateToConfigMap(ctx)
		cancel()
	}
}
