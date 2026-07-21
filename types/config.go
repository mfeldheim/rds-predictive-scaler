package types

import "time"

type Config struct {
	AwsRegion             string        `json:"aws_region"`
	RdsClusterName        string        `json:"rds_cluster_name"`
	InstanceNamePrefix    string        `json:"instance_name_prefix"`
	MaxInstances          uint          `json:"max_instances"`
	MinInstances          uint          `json:"min_instances"`
	BoostHours            string        `json:"boost_hours"`
	TargetCpuUtil         float64       `json:"target_cpu_util"`
	PlanAheadTime         time.Duration `json:"plan_ahead_time"`
	ServerPort            uint          `json:"server_port"`
	ReaderInstanceClasses string        `json:"reader_instance_classes"`
	EnableBalancing       bool          `json:"enable_balancing"`
	BalancingInterval     time.Duration `json:"balancing_interval"`
	EnableAutoPatch       bool          `json:"enable_auto_patch"`
}
