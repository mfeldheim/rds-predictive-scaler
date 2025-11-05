interface Config {
    aws_region: string;
    rds_cluster_name: string;
    instance_name_prefix: string;
    max_instances: number;
    min_instances: number;
    boost_hours: string;
    target_cpu_util: number;
    plan_ahead_time: number;
    server_port: number;
    reader_instance_classes: string;
    enable_balancing: boolean;
    balancing_interval: number;
}