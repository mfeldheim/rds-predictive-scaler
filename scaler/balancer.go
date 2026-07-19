package scaler

import (
	"fmt"
	"time"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/rds"
)

// determineOptimalWriterClass determines which instance class should be used for the writer
// based on available reserved instances and configured priorities
func (s *Scaler) determineOptimalWriterClass(instanceClasses []string) (string, error) {
	if len(instanceClasses) == 0 {
		return "", nil // No configuration, no optimization
	}

	// Normalize all instance classes to include "db." prefix
	normalizedClasses := make([]string, len(instanceClasses))
	for i, class := range instanceClasses {
		normalizedClasses[i] = normalizeInstanceClass(class)
	}

	// Get reserved instance counts from AWS API
	reservedCounts, err := s.getReservedInstanceCounts()
	if err != nil {
		s.logger.Warn().Err(err).Msg("Failed to get reserved instance counts for balancing")
		// Fall back to first configured class
		return normalizedClasses[0], nil
	}

	// Find the highest priority class with available RIs
	for _, instanceClass := range normalizedClasses {
		reservedCount := reservedCounts[instanceClass]
		if reservedCount > 0 {
			s.logger.Debug().
				Str("InstanceClass", instanceClass).
				Int("ReservedCount", reservedCount).
				Msg("Found optimal writer class with available RI")
			return instanceClass, nil
		}
	}

	// No RIs available, use first configured class (highest priority)
	s.logger.Debug().
		Str("InstanceClass", normalizedClasses[0]).
		Msg("No RIs available, using highest priority class for writer")
	return normalizedClasses[0], nil
}

// findTargetInstanceForBalancing finds a reader instance with the optimal class to promote to writer
func (s *Scaler) findTargetInstanceForBalancing(optimalClass string) (*rds.DBInstance, error) {
	// Get all reader instances
	readers, err := s.getReaderInstances(StatusAvailable)
	if err != nil {
		return nil, fmt.Errorf("failed to get reader instances: %v", err)
	}

	// Find a reader with the optimal instance class
	for _, reader := range readers {
		if aws.StringValue(reader.DBInstanceClass) == optimalClass {
			s.logger.Info().
				Str("InstanceIdentifier", aws.StringValue(reader.DBInstanceIdentifier)).
				Str("InstanceClass", optimalClass).
				Msg("Found target instance for balancing")
			return reader, nil
		}
	}

	return nil, fmt.Errorf("no available reader with instance class %s", optimalClass)
}

// shouldBalance checks if balancing is needed and returns the target instance to promote
func (s *Scaler) shouldBalance() (bool, *rds.DBInstance, error) {
	// Check if reader instance classes are configured
	if s.config.ReaderInstanceClasses == "" {
		s.logger.Debug().Msg("Reader instance classes not configured, skipping balancing")
		return false, nil, nil
	}

	// Parse reader instance classes configuration
	instanceClasses, err := parseReaderInstanceClasses(s.config.ReaderInstanceClasses)
	if err != nil {
		s.logger.Error().Err(err).Msg("Error parsing reader instance classes for balancing")
		return false, nil, nil
	}

	if len(instanceClasses) == 0 {
		s.logger.Debug().Msg("No instance classes configured, skipping balancing")
		return false, nil, nil
	}

	// Get current writer instance
	writerInstance, err := s.getWriterInstance()
	if err != nil {
		return false, nil, fmt.Errorf("failed to get writer instance: %v", err)
	}

	currentWriterClass := aws.StringValue(writerInstance.DBInstanceClass)

	// Determine optimal writer class
	optimalClass, err := s.determineOptimalWriterClass(instanceClasses)
	if err != nil {
		return false, nil, fmt.Errorf("failed to determine optimal writer class: %v", err)
	}

	// Check if current writer is already optimal
	if currentWriterClass == optimalClass {
		s.logger.Debug().
			Str("CurrentClass", currentWriterClass).
			Str("OptimalClass", optimalClass).
			Msg("Writer is already using optimal instance class, no balancing needed")
		return false, nil, nil
	}

	// Find a reader instance with the optimal class to promote
	targetInstance, err := s.findTargetInstanceForBalancing(optimalClass)
	if err != nil {
		s.logger.Info().
			Str("OptimalClass", optimalClass).
			Err(err).
			Msg("No suitable reader found for balancing, will wait for next scaling event")
		return false, nil, nil
	}

	s.logger.Info().
		Str("CurrentWriterClass", currentWriterClass).
		Str("OptimalWriterClass", optimalClass).
		Str("TargetInstance", aws.StringValue(targetInstance.DBInstanceIdentifier)).
		Msg("Balancing needed: will promote reader to writer")

	return true, targetInstance, nil
}

// checkUnusedRIs checks if there are available RIs that are not being used
// Returns true if we should create a new reader to use an available RI
func (s *Scaler) checkUnusedRIs() (bool, string, error) {
	// Check if reader instance classes are configured
	if s.config.ReaderInstanceClasses == "" {
		return false, "", nil
	}

	// Parse reader instance classes configuration
	instanceClasses, err := parseReaderInstanceClasses(s.config.ReaderInstanceClasses)
	if err != nil {
		return false, "", err
	}

	if len(instanceClasses) == 0 {
		return false, "", nil
	}

	// Normalize all instance classes
	normalizedClasses := make([]string, len(instanceClasses))
	for i, class := range instanceClasses {
		normalizedClasses[i] = normalizeInstanceClass(class)
	}

	// Get reserved instance counts
	reservedCounts, err := s.getReservedInstanceCounts()
	if err != nil {
		s.logger.Warn().Err(err).Msg("Failed to get reserved instance counts for unused RI check")
		return false, "", nil
	}

	// Get all current instances (writer + readers)
	allInstances, err := s.getAllInstances()
	if err != nil {
		return false, "", fmt.Errorf("failed to get all instances: %v", err)
	}

	// Count current instances by class
	currentCounts := s.countInstancesByClass(allInstances)

	// Check each configured class for unused RIs
	for _, instanceClass := range normalizedClasses {
		reservedCount := reservedCounts[instanceClass]
		currentCount := currentCounts[instanceClass]
		availableRI := reservedCount - currentCount

		if availableRI > 0 {
			s.logger.Info().
				Str("InstanceClass", instanceClass).
				Int("ReservedCount", reservedCount).
				Int("CurrentCount", currentCount).
				Int("AvailableRI", availableRI).
				Msg("Found unused reserved instance")
			return true, instanceClass, nil
		}
	}

	return false, "", nil
}

// getAllInstances returns all instances in the cluster (writer + readers)
func (s *Scaler) getAllInstances() ([]*rds.DBInstance, error) {
	describeInput := &rds.DescribeDBInstancesInput{
		Filters: []*rds.Filter{
			{
				Name:   aws.String("db-cluster-id"),
				Values: []*string{aws.String(s.config.RdsClusterName)},
			},
		},
	}

	describeOutput, err := s.rdsClient.DescribeDBInstances(describeInput)
	if err != nil {
		return nil, fmt.Errorf("error describing RDS instances: %v", err)
	}

	return describeOutput.DBInstances, nil
}

// performBalancing executes the balancing operation by failing over to the optimal instance
// and creating new readers to use available RIs
func (s *Scaler) performBalancing() error {
	s.logger.Info().Msg("Starting balancing check")

	// Check if balancing is enabled
	if !s.config.EnableBalancing {
		s.logger.Debug().Msg("Balancing is disabled, skipping")
		return nil
	}

	// Never run balancing while patch mode is active – it would race with failovers
	// and temp-reader creation done by the patcher.
	s.patchMu.Lock()
	patchActive := s.patchStatus.Active
	s.patchMu.Unlock()
	if patchActive {
		s.logger.Debug().Msg("Patch mode active, skipping balancing")
		return nil
	}

	// Check if scaling is in progress
	if s.scalerStatus.IsScaling {
		s.logger.Debug().Msg("Scaling operation in progress, skipping balancing")
		return nil
	}

	// First, check if we should create a new reader to use an available RI
	shouldCreateReader, riClass, err := s.checkUnusedRIs()
	if err != nil {
		s.logger.Error().Err(err).Msg("Error checking for unused RIs")
	}

	if shouldCreateReader {
		// Check if we're at max instances
		readerInstances, err := s.getReaderInstances(StatusAll ^ StatusDeleting)
		if err != nil {
			s.logger.Error().Err(err).Msg("Failed to get reader instances for RI balancing")
		} else if (len(readerInstances) + 1) < int(s.config.MaxInstances) {
			s.logger.Info().
				Str("InstanceClass", riClass).
				Msg("Creating new reader to use available reserved instance")

			// Set scaling flag
			s.scalerStatus.IsScaling = true
			defer func() {
				s.scalerStatus.IsScaling = false
			}()

			// Get writer instance for template
			writerInstance, err := s.getWriterInstance()
			if err != nil {
				return fmt.Errorf("failed to get writer instance: %v", err)
			}

			// Generate instance name
			currentHour := time.Now().In(time.UTC).Hour()
			randomUID := generateRandomUID()
			readerName := fmt.Sprintf("%s%d-%s", s.config.InstanceNamePrefix, currentHour, randomUID)

			// Create the reader with the RI class
			_, err = s.createReaderInstance(readerName, writerInstance, riClass)
			if err != nil {
				return fmt.Errorf("failed to create reader for RI balancing: %v", err)
			}

			s.logger.Info().
				Str("ReaderName", readerName).
				Str("InstanceClass", riClass).
				Msg("Created reader to use available reserved instance")

			// Don't continue with writer balancing in the same cycle
			return nil
		} else {
			s.logger.Info().
				Str("InstanceClass", riClass).
				Msg("Available RI found but max instances reached, skipping reader creation")
		}
	}

	// Check if writer balancing is needed
	shouldBalance, targetInstance, err := s.shouldBalance()
	if err != nil {
		return fmt.Errorf("error checking if balancing needed: %v", err)
	}

	if !shouldBalance {
		s.logger.Debug().Msg("Writer balancing not needed")
		return nil
	}

	// Set scaling flag to prevent conflicts
	s.scalerStatus.IsScaling = true
	defer func() {
		s.scalerStatus.IsScaling = false
	}()

	// Perform failover to the target instance
	targetInstanceId := aws.StringValue(targetInstance.DBInstanceIdentifier)
	s.logger.Info().
		Str("TargetInstance", targetInstanceId).
		Str("TargetClass", aws.StringValue(targetInstance.DBInstanceClass)).
		Msg("Initiating failover for balancing")

	err = s.failoverToInstance(targetInstanceId)
	if err != nil {
		return fmt.Errorf("failed to failover to instance %s: %v", targetInstanceId, err)
	}

	s.logger.Info().
		Str("NewWriter", targetInstanceId).
		Str("NewWriterClass", aws.StringValue(targetInstance.DBInstanceClass)).
		Msg("Balancing completed successfully")

	return nil
}

