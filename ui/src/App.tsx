import {useEffect, useState} from 'react';
import {createTheme, ThemeProvider} from '@mui/material/styles';
import CssBaseline from '@mui/material/CssBaseline';
import Box from '@mui/material/Box';
import {AppBar, Badge, Container, Grid, IconButton, Paper, Toolbar, Typography} from "@mui/material";

import NotificationsIcon from '@mui/icons-material/Notifications';
import MenuIcon from '@mui/icons-material/Menu';

import Copyright from "./components/Copyright.tsx";
import Loading from "./components/Loading.tsx";
import useWebSocket from "react-use-websocket";
import CurrentLoad from "./components/CurrentLoad.tsx";
import CurrentSize from "./components/CurrentSize.tsx";
import ClusterMap from "./components/ClusterMap.tsx";
import GraphUtilization from "./components/GraphUtilization.tsx";
import GraphClusterSize from "./components/GraphClusterSize.tsx";
import Broadcast from "./types/Broadcast.ts";
import PatchModePanel from "./components/PatchModePanel.tsx";

const theme = createTheme({
    palette: {
        mode: 'dark',
        background: {
            default: '#05080f',
            paper: 'rgba(255, 255, 255, 0.02)',
        },
        primary: {
            main: '#60a5fa',
        },
        secondary: {
            main: '#34d399',
        },
        divider: 'rgba(255, 255, 255, 0.06)',
        text: {
            primary: 'rgba(255, 255, 255, 0.92)',
            secondary: 'rgba(255, 255, 255, 0.56)',
        },
    },
    shape: {
        borderRadius: 16,
    },
    typography: {
        fontFamily: '"Inter", "Public Sans", sans-serif',
        h6: {
            fontWeight: 700,
            letterSpacing: '-0.01em',
        },
    },
    components: {
        MuiCssBaseline: {
            styleOverrides: {
                body: {
                    backgroundColor: 'transparent',
                },
            },
        },
        MuiAppBar: {
            styleOverrides: {
                root: {
                    backgroundColor: 'rgba(5, 8, 15, 0.72)',
                    backdropFilter: 'blur(16px)',
                    WebkitBackdropFilter: 'blur(16px)',
                    borderBottom: '1px solid rgba(255, 255, 255, 0.06)',
                    boxShadow: 'none',
                },
            },
        },
        MuiPaper: {
            styleOverrides: {
                root: {
                    backgroundColor: 'rgba(255, 255, 255, 0.02)',
                    backgroundImage: 'linear-gradient(180deg, rgba(255,255,255,0.035), rgba(255,255,255,0))',
                    border: '1px solid rgba(255, 255, 255, 0.06)',
                    backdropFilter: 'blur(20px)',
                    WebkitBackdropFilter: 'blur(20px)',
                    boxShadow: '0 1px 0 0 rgba(255,255,255,0.04) inset, 0 24px 48px -24px rgba(0,0,0,0.6)',
                    transition: 'transform 0.2s ease, box-shadow 0.2s ease, border-color 0.2s ease',
                },
            },
        },
    },
});

function App() {
    const socketUrl = `ws://${window.location.host}${window.location.pathname.replace(/\/$/, '')}/ws`;

    const {lastMessage, readyState, sendMessage} = useWebSocket(socketUrl, {
        onOpen: () => {
            console.log('WebSocket connection established');
        },
        shouldReconnect: () => true,
    });

    const [appBarOpen, setAppBarOpen] = useState(true);

    const [config, setConfig] = useState<Config | null>(null);
    const [clusterStatus, setClusterStatus] = useState<ClusterStatus | null>(null);
    const [clusterStatusPrediction, setClusterStatusPrediction] = useState<ClusterStatus | null>(null);
    const [clusterStatusHistory, setClusterStatusHistory] = useState<ClusterStatus[]>([]);
    const [clusterStatusPredictionHistory, setClusterStatusPredictionHistory] = useState<ClusterStatus[]>([]);
    const [patchStatus, setPatchStatus] = useState<PatchStatus | null>(null);

    const toggleDrawer = () => {
        setAppBarOpen(!appBarOpen);
    };

    useEffect(() => {
        if (lastMessage === null) return;
        const broadcast = JSON.parse(lastMessage.data) as Broadcast;

        switch (broadcast.type) {
            case 'config':
                setConfig(broadcast.data);
                break;
            case 'clusterStatus':
                setClusterStatus(broadcast.data);
                if (clusterStatusHistory.length > 0) {
                    setClusterStatusHistory((prev) => [...prev.slice(1), broadcast.data])
                }
                break;
            case 'clusterStatusPrediction':
                setClusterStatusPrediction(broadcast.data);
                if (clusterStatusPredictionHistory.length > 0) {
                    setClusterStatusPredictionHistory((prev) => [...prev.slice(1), broadcast.data])
                }
                break;
            case 'clusterStatusPredictionHistory':
                setClusterStatusPredictionHistory(broadcast.data);
                break;
            case 'clusterStatusHistory':
                setClusterStatusHistory(broadcast.data);
                break;
            case 'patchStatus':
                setPatchStatus(broadcast.data);
                break;
        }
    }, [lastMessage]);

    const aggregatedHistory = groupDataByTime(clusterStatusHistory, 5 * 60 * 1000);
    const aggregatedPredictionHistory = groupDataByTime(clusterStatusPredictionHistory, 5 * 60 * 1000);

    return (
        <ThemeProvider theme={theme}>
            {readyState === 1 && clusterStatus ? (
                <Box sx={{display: 'flex'}}>
                    <CssBaseline/>
                    <AppBar>
                        <Toolbar
                            sx={{
                                pr: '24px', // keep right padding when drawer closed
                            }}
                        >
                            <IconButton
                                edge="start"
                                color="inherit"
                                aria-label="open drawer"
                                onClick={toggleDrawer}
                                sx={{
                                    marginRight: '36px',
                                    ...(appBarOpen && {display: 'none'}),
                                }}
                            >
                                <MenuIcon/>
                            </IconButton>
                            <Box sx={{display: 'flex', alignItems: 'center', gap: 2, flexGrow: 1}}>
                                <Box
                                    component="img"
                                    src="/logo_transparent.png"
                                    alt="Logo"
                                    sx={{
                                        height: 36,
                                        width: 'auto',
                                        filter: 'drop-shadow(0 0 12px rgba(96,165,250,0.35))',
                                    }}
                                />
                                <Typography
                                    component="h1"
                                    variant="h6"
                                    noWrap
                                    className="gradient-text"
                                    sx={{fontWeight: 800, letterSpacing: '-0.01em'}}
                                >
                                    RDS Predictive Scaler
                                </Typography>
                                <Box
                                    className="pulse-dot"
                                    sx={{
                                        width: 8,
                                        height: 8,
                                        borderRadius: '50%',
                                        backgroundColor: '#34d399',
                                        boxShadow: '0 0 0 4px rgba(52,211,153,0.15)',
                                        ml: 0.5,
                                    }}
                                />
                            </Box>
                            <IconButton color="inherit">
                                <Badge badgeContent={4} color="secondary">
                                    <NotificationsIcon/>
                                </Badge>
                            </IconButton>
                        </Toolbar>
                        <Box className="gradient-divider"/>
                    </AppBar>

                    <Box
                        component="main"
                        sx={{
                            backgroundColor: 'transparent',
                            flexGrow: 1,
                            height: '100vh',
                            overflow: 'auto',
                            width: '100%'
                        }}
                    >
                        <Toolbar/>
                        <Container sx={{width: '100%', maxWidth: '1400px !important', py: 4}}>
                            <Grid container spacing={3} sx={{width: '100%'}}>
                                <Grid item xs={12} md={6}>
                                    <Paper
                                        className="fade-up"
                                        sx={{
                                            p: 2.5,
                                            display: 'flex',
                                            flexDirection: 'column',
                                            height: '100%',
                                            '&:hover': {
                                                transform: 'translateY(-2px)',
                                                borderColor: 'rgba(96, 165, 250, 0.25)',
                                                boxShadow: '0 1px 0 0 rgba(255,255,255,0.05) inset, 0 28px 56px -24px rgba(0,0,0,0.7), 0 0 0 1px rgba(96,165,250,0.08)',
                                            },
                                        }}
                                    >
                                        <CurrentLoad status={clusterStatus} prediction={clusterStatusPrediction}/>
                                    </Paper>
                                </Grid>

                                <Grid item xs={12} md={6}>
                                    <Paper
                                        className="fade-up"
                                        sx={{
                                            p: 2.5,
                                            display: 'flex',
                                            flexDirection: 'column',
                                            height: '100%',
                                            animationDelay: '0.05s',
                                            '&:hover': {
                                                transform: 'translateY(-2px)',
                                                borderColor: 'rgba(52, 211, 153, 0.25)',
                                                boxShadow: '0 1px 0 0 rgba(255,255,255,0.05) inset, 0 28px 56px -24px rgba(0,0,0,0.7), 0 0 0 1px rgba(52,211,153,0.08)',
                                            },
                                        }}
                                    >
                                        <CurrentSize status={clusterStatus} prediction={clusterStatusPrediction}
                                                     config={config}/>
                                    </Paper>
                                </Grid>

                                <Grid item xs={12}>
                                    <Paper className="fade-up" sx={{p: 2.5, display: 'flex', flexDirection: 'column', animationDelay: '0.1s'}}>
                                        <GraphUtilization historyData={aggregatedHistory}
                                                          predictionData={aggregatedPredictionHistory}
                                                          targetCpuUtilization={config?.target_cpu_util}/>
                                        <GraphClusterSize data={aggregatedHistory}/>
                                    </Paper>
                                </Grid>

                                <Grid item xs={12} className="fade-up" sx={{animationDelay: '0.15s'}}>
                                    <ClusterMap clusterStatus={clusterStatus}/>
                                </Grid>

                                <Grid item xs={12}>
                                    <Paper className="fade-up" sx={{p: 2.5, display: 'flex', flexDirection: 'column', animationDelay: '0.2s'}}>
                                        <PatchModePanel
                                            patchStatus={patchStatus}
                                            autoPatchEnabled={config?.enable_auto_patch ?? true}
                                            onStartPatch={() => sendMessage(JSON.stringify({type: 'start_patch_mode', data: null}))}
                                            onStopPatch={() => sendMessage(JSON.stringify({type: 'stop_patch_mode', data: null}))}
                                            clusterInstances={clusterStatus?.instance_status ?? []}
                                        />
                                    </Paper>
                                </Grid>
                            </Grid>

                            <Copyright sx={{pt: 4}}/>
                        </Container>
                    </Box>
                </Box>) : <Loading readyState={readyState}/>}
        </ThemeProvider>
    );
}

function groupDataByTime(data: ClusterStatus[], interval: number): ClusterStatus[] {
    const groupedData: ClusterStatus[] = [];
    let currentGroup: ClusterStatus | null = null;

    let count = 0;
    let sizeSum = 0;
    let optimalSizeSum = 0;
    let utilizationSum = 0;

    data.forEach((status) => {
        const timestamp = new Date(status.timestamp).getTime();
        // Determine the appropriate time interval based on the conditions

        if (!currentGroup) {
            currentGroup = {
                ...status,
                timestamp: status.timestamp,
                current_active_readers: 0,
            } as ClusterStatus;
            sizeSum = status.current_active_readers;
            optimalSizeSum = status.optimal_size;
            utilizationSum = status.average_cpu_utilization;
            count = 1;
        } else if (timestamp - new Date(currentGroup.timestamp).getTime() >= interval) {
            (currentGroup as ClusterStatus).current_active_readers = sizeSum / count;
            groupedData.push(currentGroup);
            currentGroup = {...status, timestamp: status.timestamp, current_active_readers: 0};
            sizeSum = status.current_active_readers;
            optimalSizeSum = status.optimal_size;
            utilizationSum = status.average_cpu_utilization;
            count = 1;
        } else {
            sizeSum += status.current_active_readers;
            optimalSizeSum += status.optimal_size;
            utilizationSum += status.average_cpu_utilization;
            count++;
        }
    });

    if (currentGroup) {
        (currentGroup as ClusterStatus).current_active_readers = sizeSum / count;
        (currentGroup as ClusterStatus).average_cpu_utilization = utilizationSum / count;
        (currentGroup as ClusterStatus).optimal_size = optimalSizeSum / count;
        groupedData.push(currentGroup);
    }

    return groupedData;
}

export default App;
