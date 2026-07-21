package types

type PatchInstanceInfo struct {
	Identifier        string   `json:"identifier"`
	IsWriter          bool     `json:"is_writer"`
	MaintenanceWindow string   `json:"maintenance_window"`
	PendingActions    []string `json:"pending_actions"`
}

type PatchStatus struct {
	Active             bool                `json:"active"`
	AutoPatchEnabled   bool                `json:"auto_patch_enabled"`
	TotalInstances     int                 `json:"total_instances"`
	PatchedCount       int                 `json:"patched_count"`
	CurrentlyPatching  string              `json:"currently_patching"`
	TempInstanceName   string              `json:"temp_instance_name"`
	PendingInstances   []PatchInstanceInfo `json:"pending_instances"`
	CompletedInstances []string            `json:"completed_instances"`
}
