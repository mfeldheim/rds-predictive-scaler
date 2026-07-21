package main

import (
	"flag"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"os"
	"os/signal"
	"predictive-rds-scaler/api"
	"predictive-rds-scaler/logging"
	"predictive-rds-scaler/scaler"
	"predictive-rds-scaler/types"
	"syscall"
	"time"
)

var conf = &types.Config{}

func init() {
	flag.StringVar(&conf.RdsClusterName, "rdsClusterName", "", "RDS cluster name")
	flag.StringVar(&conf.InstanceNamePrefix, "instanceNamePrefix", "predictive-autoscaling-", "Prefix for reader instance names")
	flag.StringVar(&conf.AwsRegion, "awsRegion", "", "AWS region")

	flag.Float64Var(&conf.TargetCpuUtil, "targetCpuUtilization", 70.0, "Target CPU utilization percentage")
	flag.StringVar(&conf.BoostHours, "boostHours", "", "Comma-separated list of hours to boost minInstances")
	flag.DurationVar(&conf.PlanAheadTime, "planAheadTime", 10*time.Minute, "The time to plan ahead when looking up prior CPU utilization")
	flag.UintVar(&conf.MinInstances, "minInstances", 2, "Minimum number of readers required in the cluster")
	flag.UintVar(&conf.MaxInstances, "maxInstances", 5, "Maximum number of readers allowed in the cluster")
	flag.StringVar(&conf.ReaderInstanceClasses, "readerInstanceClasses", "", "Comma-separated list of reader instance classes (e.g., r8g.xlarge,r7g.xlarge,r6g.xlarge). Scaler will use available reserved instances first, then fall back to first class for on-demand.")
	flag.BoolVar(&conf.EnableBalancing, "enableBalancing", false, "Enable periodic balancing to optimize writer instance class based on available RIs")
	flag.DurationVar(&conf.BalancingInterval, "balancingInterval", 5*time.Minute, "Interval for running balancing checks")
	flag.BoolVar(&conf.EnableAutoPatch, "enableAutoPatch", true, "Automatically apply pending maintenance during each instance's maintenance window")

	flag.UintVar(&conf.ServerPort, "serverPort", 8041, "Port for the ui server")

	flag.Parse()
}

func main() {
	// Initialize logger
	logging.InitLogger()
	logger := logging.GetLogger()

	// Create AWS session
	awsSession, err := session.NewSessionWithOptions(session.Options{
		SharedConfigState: session.SharedConfigEnable,
		Config: aws.Config{
			Region: aws.String(conf.AwsRegion),
		},
	})

	if err != nil {
		logger.Error().Err(err).Msg("Failed to create AWS session")
		return
	}

	// Create broadcast channel
	broadcast := make(chan types.Broadcast)

	// Create and start the scaler
	rdsScaler, err := scaler.New(conf, logger, awsSession, broadcast)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to create scaler")
		return
	}

	// Log full configuration
	logger.Info().
		Str("AwsRegion", conf.AwsRegion).
		Str("RdsClusterName", conf.RdsClusterName).
		Str("InstanceNamePrefix", conf.InstanceNamePrefix).
		Uint("MinInstances", conf.MinInstances).
		Uint("MaxInstances", conf.MaxInstances).
		Float64("TargetCpuUtil", conf.TargetCpuUtil).
		Dur("PlanAheadTime", conf.PlanAheadTime).
		Str("ReaderInstanceClasses", conf.ReaderInstanceClasses).
		Bool("EnableBalancing", conf.EnableBalancing).
		Dur("BalancingInterval", conf.BalancingInterval).
		Bool("EnableAutoPatch", conf.EnableAutoPatch).
		Msg("Starting RDS Predictive Scaler with configuration")

	// Create and start the API server
	apiServer := api.New(conf, logger, broadcast)
	apiServer.OnClientConnect(initialBroadcasts(rdsScaler))
	apiServer.OnPatchAction(rdsScaler.StartPatchMode, rdsScaler.StopPatchMode)

	go func() {
		err = apiServer.Serve(conf.ServerPort)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to start API server")
		}
	}()

	// Start balancing ticker if enabled
	var balancingTicker *time.Ticker
	if conf.EnableBalancing {
		logger.Info().
			Dur("Interval", conf.BalancingInterval).
			Msg("Balancing enabled, starting balancing ticker")

		// Run balancing immediately on startup
		go func() {
			logger.Info().Msg("Running initial balancing check")
			err := rdsScaler.PerformBalancing()
			if err != nil {
				logger.Error().Err(err).Msg("Error during initial balancing")
			}
		}()

		// Then run periodically
		balancingTicker = time.NewTicker(conf.BalancingInterval)
		go func() {
			for range balancingTicker.C {
				err := rdsScaler.PerformBalancing()
				if err != nil {
					logger.Error().Err(err).Msg("Error during balancing")
				}
			}
		}()
	} else {
		logger.Info().Msg("Balancing disabled")
	}

	// Start the scaler (blocking call)
	rdsScaler.Run()

	// Set up a channel to capture termination signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// Block until a termination signal is received
	<-sigCh

	// Handle the termination signal by initiating graceful shutdown
	logger.Info().Msg("Received termination signal. Initiating graceful shutdown...")

	// Stop the balancing ticker if it was started
	if balancingTicker != nil {
		balancingTicker.Stop()
		logger.Info().Msg("Balancing ticker stopped")
	}

	// Stop the API server and the scaler
	apiServer.Stop()
	rdsScaler.Stop()

	logger.Info().Msg("Shutdown complete. Exiting.")
}

func initialBroadcasts(rdsScaler *scaler.Scaler) func() []types.Broadcast {
	return func() []types.Broadcast {
		var broadcasts []types.Broadcast
		broadcasts = append(broadcasts, types.Broadcast{MessageType: "config", Data: conf})
		broadcasts = append(broadcasts, types.Broadcast{MessageType: "patchStatus", Data: rdsScaler.GetPatchStatus()})

		clusterStatusHistory := rdsScaler.GetClusterStatusHistory(24 * time.Hour)
		if clusterStatusHistory != nil {
			broadcasts = append(broadcasts, types.Broadcast{MessageType: "clusterStatusHistory", Data: clusterStatusHistory})
		}

		clusterStatusPredictionHistory := rdsScaler.GetClusterStatusPredictionHistory(24 * time.Hour)
		if clusterStatusPredictionHistory != nil {
			broadcasts = append(broadcasts, types.Broadcast{MessageType: "clusterStatusPredictionHistory", Data: clusterStatusPredictionHistory})
		}

		return broadcasts
	}
}
