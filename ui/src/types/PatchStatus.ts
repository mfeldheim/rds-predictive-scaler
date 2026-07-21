interface PatchInstanceInfo {
    identifier: string;
    is_writer: boolean;
    maintenance_window: string;
    pending_actions: string[];
}

interface PatchStatus {
    active: boolean;
    auto_patch_enabled: boolean;
    total_instances: number;
    patched_count: number;
    currently_patching: string;
    temp_instance_name: string;
    pending_instances: PatchInstanceInfo[];
    completed_instances: string[];
}
