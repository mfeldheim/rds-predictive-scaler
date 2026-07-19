import React from 'react';
import {
    Box,
    Button,
    Chip,
    CircularProgress,
    Divider,
    LinearProgress,
    List,
    ListItem,
    ListItemIcon,
    ListItemText,
    Stack,
    Switch,
    Tooltip,
    Typography,
} from '@mui/material';
import {
    CheckCircle,
    HourglassEmpty,
    Loop,
    PendingActions,
    PlayArrow,
    Stop,
    UpdateDisabled,
} from '@mui/icons-material';
import {green, grey, orange} from '@mui/material/colors';

interface PatchModePanelProps {
    patchStatus: PatchStatus | null;
    autoPatchEnabled: boolean;
    onStartPatch: () => void;
    onStopPatch: () => void;
    clusterInstances: InstanceStatus[];
}

const PatchModePanel: React.FC<PatchModePanelProps> = ({
    patchStatus,
    autoPatchEnabled,
    onStartPatch,
    onStopPatch,
    clusterInstances,
}) => {
    const active = patchStatus?.active ?? false;
    const total = patchStatus?.total_instances ?? 0;
    const done = patchStatus?.patched_count ?? 0;
    const progress = total > 0 ? Math.round((done / total) * 100) : 0;
    const currentlyPatching = patchStatus?.currently_patching ?? '';
    const tempInstance = patchStatus?.temp_instance_name ?? '';
    const completed = patchStatus?.completed_instances ?? [];

    // Instances from the live cluster status that have pending patches
    const instancesWithPending = clusterInstances.filter(
        (i) => i.pending_actions && i.pending_actions.length > 0
    );

    const hasPendingPatch = instancesWithPending.length > 0;

    return (
        <Box>
            <Stack direction="row" alignItems="center" justifyContent="space-between" mb={1}>
                <Typography variant="h5" component="div" sx={{display: 'flex', alignItems: 'center', gap: 1}}>
                    <PendingActions fontSize="large" />
                    Maintenance &amp; Patching
                </Typography>

                <Stack direction="row" alignItems="center" spacing={1}>
                    <Tooltip title="Auto-patch: automatically applies pending maintenance during each instance's configured maintenance window">
                        <Stack direction="row" alignItems="center" spacing={0.5}>
                            <Typography variant="caption" color="text.secondary">Auto-patch</Typography>
                            <Switch
                                size="small"
                                checked={autoPatchEnabled}
                                disabled
                                color="success"
                            />
                        </Stack>
                    </Tooltip>

                    {active ? (
                        <Button
                            variant="outlined"
                            color="warning"
                            size="small"
                            startIcon={<Stop />}
                            onClick={onStopPatch}
                        >
                            Stop Patching
                        </Button>
                    ) : (
                        <Tooltip title={!hasPendingPatch ? 'No pending maintenance actions found' : 'Start rolling patch of all instances with pending maintenance'}>
                            <span>
                                <Button
                                    variant="contained"
                                    color="primary"
                                    size="small"
                                    startIcon={<PlayArrow />}
                                    onClick={onStartPatch}
                                    disabled={!hasPendingPatch}
                                >
                                    Start Patching
                                </Button>
                            </span>
                        </Tooltip>
                    )}
                </Stack>
            </Stack>

            <Divider variant="middle" sx={{mb: 2}} />

            {/* Active patch progress */}
            {active && (
                <Box mb={2}>
                    <Stack direction="row" alignItems="center" justifyContent="space-between" mb={0.5}>
                        <Typography variant="body2" color="text.secondary">
                            Progress: {done} / {total} instances patched
                        </Typography>
                        <Typography variant="body2" color="text.secondary">{progress}%</Typography>
                    </Stack>
                    <LinearProgress variant="determinate" value={progress} sx={{height: 8, borderRadius: 4}} />

                    {currentlyPatching && (
                        <Stack direction="row" alignItems="center" spacing={1} mt={1}>
                            <CircularProgress size={16} />
                            <Typography variant="body2">
                                Patching: <strong>{currentlyPatching}</strong>
                            </Typography>
                            {tempInstance && (
                                <Chip
                                    label={`+1 temp: ${tempInstance}`}
                                    size="small"
                                    color="info"
                                    icon={<Loop fontSize="small" />}
                                />
                            )}
                        </Stack>
                    )}
                </Box>
            )}

            {/* Per-instance status list */}
            {clusterInstances.length > 0 && (
                <List dense disablePadding>
                    {clusterInstances
                        .filter((i) => !i.identifier.includes('patch-')) // hide temp instances from this list
                        .map((inst) => {
                            const isCurrentlyPatching = inst.identifier === currentlyPatching;
                            const isDone = completed.includes(inst.identifier);
                            const hasPending = inst.pending_actions && inst.pending_actions.length > 0;

                            let icon: React.ReactNode;
                            let chipColor: 'success' | 'warning' | 'default' | 'info' = 'default';

                            if (isCurrentlyPatching) {
                                icon = <CircularProgress size={18} />;
                                chipColor = 'info';
                            } else if (isDone) {
                                icon = <CheckCircle sx={{color: green[400]}} />;
                                chipColor = 'success';
                            } else if (hasPending) {
                                icon = <HourglassEmpty sx={{color: orange[400]}} />;
                                chipColor = 'warning';
                            } else {
                                icon = <UpdateDisabled sx={{color: grey[500]}} />;
                            }

                            return (
                                <ListItem key={inst.identifier} sx={{py: 0.25}}>
                                    <ListItemIcon sx={{minWidth: 32}}>{icon}</ListItemIcon>
                                    <ListItemText
                                        primary={
                                            <Stack direction="row" alignItems="center" spacing={1}>
                                                <Typography variant="body2">
                                                    {inst.identifier}
                                                    {inst.is_writer && (
                                                        <Chip label="writer" size="small" sx={{ml: 0.5, height: 16, fontSize: 10}} />
                                                    )}
                                                </Typography>
                                            </Stack>
                                        }
                                        secondary={
                                            <Stack direction="row" spacing={0.5} flexWrap="wrap" alignItems="center">
                                                {inst.maintenance_window && (
                                                    <Typography variant="caption" color="text.secondary">
                                                        window: {inst.maintenance_window}
                                                    </Typography>
                                                )}
                                                {hasPending &&
                                                    inst.pending_actions.map((action) => (
                                                        <Chip
                                                            key={action}
                                                            label={action}
                                                            size="small"
                                                            color={chipColor}
                                                            sx={{height: 16, fontSize: 10}}
                                                        />
                                                    ))}
                                                {isDone && (
                                                    <Chip label="patched" size="small" color="success" sx={{height: 16, fontSize: 10}} />
                                                )}
                                            </Stack>
                                        }
                                    />
                                </ListItem>
                            );
                        })}
                </List>
            )}

            {!active && !hasPendingPatch && (
                <Box sx={{textAlign: 'center', py: 2}}>
                    <Typography variant="body2" color="text.secondary">
                        All instances are up to date. No pending maintenance actions.
                    </Typography>
                </Box>
            )}

            {!active && hasPendingPatch && !autoPatchEnabled && (
                <Box sx={{mt: 1, p: 1, bgcolor: 'background.paper', borderRadius: 1, border: '1px solid', borderColor: 'warning.main'}}>
                    <Typography variant="caption" color="warning.main">
                        Auto-patch is disabled. Pending maintenance will not be applied automatically.
                        Use "Start Patching" to apply now.
                    </Typography>
                </Box>
            )}
        </Box>
    );
};

export default PatchModePanel;
