interface InstanceStatus {
    identifier: string;
    is_writer: boolean;
    status: string;
    cpu_utilization: number;
    maintenance_window: string;
    pending_actions: string[];
    is_patch_target: boolean;
}