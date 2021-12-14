// Copyright 2021 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/matrixorigin/matrixone/pkg/sql/compile"
	"math"

	"github.com/matrixorigin/matrixcube/storage/kv"
	"github.com/matrixorigin/matrixone/pkg/catalog"
	"github.com/matrixorigin/matrixone/pkg/config"
	"github.com/matrixorigin/matrixone/pkg/frontend"
	"github.com/matrixorigin/matrixone/pkg/logutil"
	kvDriver "github.com/matrixorigin/matrixone/pkg/vm/driver/kv"

	//"github.com/matrixorigin/matrixone/pkg/rpcserver"
	"github.com/matrixorigin/matrixone/pkg/vm/driver"
	aoeDriver "github.com/matrixorigin/matrixone/pkg/vm/driver/aoe"
	dConfig "github.com/matrixorigin/matrixone/pkg/vm/driver/config"
	"github.com/matrixorigin/matrixone/pkg/vm/driver/pb"
	"github.com/matrixorigin/matrixone/pkg/vm/engine"
	aoeEngine "github.com/matrixorigin/matrixone/pkg/vm/engine/aoe/engine"
	aoeStorage "github.com/matrixorigin/matrixone/pkg/vm/engine/aoe/storage"

	//"github.com/matrixorigin/matrixone/pkg/vm/mmu/guest"
	"github.com/matrixorigin/matrixone/pkg/vm/mmu/host"
	//"github.com/matrixorigin/matrixone/pkg/vm/process"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/cockroachdb/pebble"
	"github.com/fagongzi/log"
	"github.com/matrixorigin/matrixcube/pb/meta"
	"github.com/matrixorigin/matrixcube/server"
	cPebble "github.com/matrixorigin/matrixcube/storage/kv/pebble"
	"github.com/matrixorigin/matrixcube/vfs"
)

const (
	NormalExit              = 0
	InitialValuesExit       = 1
	LoadConfigExit          = 2
	RecreateDirExit         = 3
	DecodeAoeConfigExit     = 4
	CreateAoeExit           = 5
	DecodeCubeConfigExit    = 6
	DecodeClusterConfigExit = 7
	CreateCubeExit          = 8
	StartCubeExit           = 9
	CreateRPCExit           = 10
	WaitCubeStartExit       = 11
	StartMOExit             = 12
)

var (
	c   *catalog.Catalog
	mo  *frontend.MOServer
	pci *frontend.PDCallbackImpl
)

var (
	GoVersion    string = ""
	BranchName   string = ""
	LastCommitId string = ""
	BuildTime    string = ""
	MoVersion    string = "v0.1.0"
)

func createMOServer(callback *frontend.PDCallbackImpl) {
	address := fmt.Sprintf("%s:%d", config.GlobalSystemVariables.GetHost(), config.GlobalSystemVariables.GetPort())
	pu := config.NewParameterUnit(&config.GlobalSystemVariables, config.HostMmu, config.Mempool, config.StorageEngine, config.ClusterNodes, config.ClusterCatalog)
	mo = frontend.NewMOServer(address, pu, callback)
}

func runMOServer() error {
	return mo.Start()
}

func serverShutdown(isgraceful bool) {
	mo.Stop()
}

func registerSignalHandlers() {
	//	signal.SetupSignalHandler(serverShutdown)
}

func waitSignal() {
	sigchan := make(chan os.Signal)
	signal.Notify(sigchan, syscall.SIGTERM, syscall.SIGINT)
	<-sigchan
}

func cleanup() {
	fmt.Println("\rBye!")
}

func recreateDir(dir string) (err error) {
	//err = os.RemoveAll(dir)
	//if err != nil {
	//	return err
	//}
	mask := syscall.Umask(0)
	defer syscall.Umask(mask)
	err = os.MkdirAll(dir, os.FileMode(0755))
	return err
}

/**
call the catalog service to remove the epoch
*/
func removeEpoch(epoch uint64) {
	_, err := c.RemoveDeletedTable(epoch)
	if err != nil {
		fmt.Printf("catalog remove ddl failed. error :%v \n", err)
	}
}

func main() {
	// if the argument passed in is "--version", return version info and exit
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("MatrixOne build info:")
		fmt.Printf("The golang version used to build this binary is: %s\n", GoVersion)
		fmt.Printf("The git branch name is: %s\n", BranchName)
		fmt.Printf("Last git commit ID: %s\n", LastCommitId)
		fmt.Printf("The Buildtime is: %s\n", BuildTime)
		fmt.Printf("Current Matrixone version: %s\n", MoVersion)
		os.Exit(0)
	}

	flag.Parse()
	args := flag.Args()

	if len(args) < 1 {
		fmt.Printf("Usage: %s configFile\n", os.Args[0])
		flag.PrintDefaults()
		os.Exit(-1)
	}
	

	//close cube print info
	log.SetLevelByString("info")
	log.SetHighlighting(false)

	configFilePath := args[0]
	logutil.SetupMOLogger(configFilePath)

	//before anything using the configuration
	if err := config.GlobalSystemVariables.LoadInitialValues(); err != nil {
		logutil.Infof("Initial values error:%v\n", err)
		os.Exit(InitialValuesExit)
	}

	if err := config.LoadvarsConfigFromFile(configFilePath, &config.GlobalSystemVariables); err != nil {
		logutil.Infof("Load config error:%v\n", err)
		os.Exit(LoadConfigExit)
	}

	if *cpuProfilePathFlag != "" {
		stop := startCPUProfile()
		defer stop()
	}
	if *allocsProfilePathFlag != "" {
		defer writeAllocsProfile()
	}

	logutil.Infof("Shutdown The Server With Ctrl+C | Ctrl+\\.")

	config.HostMmu = host.New(config.GlobalSystemVariables.GetHostMmuLimitation())

	log.SetLevelByString(config.GlobalSystemVariables.GetCubeLogLevel())

	//Host := config.GlobalSystemVariables.GetHost()
	NodeId := config.GlobalSystemVariables.GetNodeID()
	strNodeId := strconv.FormatInt(NodeId, 10)

	ppu := frontend.NewPDCallbackParameterUnit(int(config.GlobalSystemVariables.GetPeriodOfEpochTimer()), int(config.GlobalSystemVariables.GetPeriodOfPersistence()), int(config.GlobalSystemVariables.GetPeriodOfDDLDeleteTimer()), int(config.GlobalSystemVariables.GetTimeoutOfHeartbeat()), config.GlobalSystemVariables.GetEnableEpochLogging(), math.MaxInt64)

	pci = frontend.NewPDCallbackImpl(ppu)
	pci.Id = int(NodeId)

	targetDir := config.GlobalSystemVariables.GetCubeDirPrefix() + strNodeId
	if err := recreateDir(targetDir); err != nil {
		logutil.Infof("Recreate dir error:%v\n", err)
		os.Exit(RecreateDirExit)
	}

	kvs, err := cPebble.NewStorage(targetDir+"/pebble/data", nil, &pebble.Options{
		FS:                          vfs.NewPebbleFS(vfs.Default),
		MemTableSize:                1024 * 1024 * 128,
		MemTableStopWritesThreshold: 4,
	})
	kvBase := kv.NewBaseStorage(kvs, vfs.Default)
	pebbleDataStorage := kv.NewKVDataStorage(kvBase, kvDriver.NewkvExecutor(kvs))

	var aoeDataStorage *aoeDriver.Storage

	opt := aoeStorage.Options{}
	_, err = toml.DecodeFile(configFilePath, &opt)
	if err != nil {
		logutil.Infof("Decode aoe config error:%v\n", err)
		os.Exit(DecodeAoeConfigExit)
	}

	catalogListener := catalog.NewCatalogListener()
	opt.EventListener = catalogListener
	aoeDataStorage, err = aoeDriver.NewStorageWithOptions(targetDir+"/aoe", &opt)
	if err != nil {
		logutil.Infof("Create aoe driver error, %v\n", err)
		os.Exit(CreateAoeExit)
	}

	cfg := dConfig.Config{}
	_, err = toml.DecodeFile(configFilePath, &cfg.CubeConfig)
	if err != nil {
		logutil.Infof("Decode cube config error:%v\n", err)
		os.Exit(DecodeCubeConfigExit)
	}

	_, err = toml.DecodeFile(configFilePath, &cfg.ClusterConfig)
	if err != nil {
		logutil.Infof("Decode cluster config error:%v\n", err)
		os.Exit(DecodeClusterConfigExit)
	}
	cfg.ServerConfig = server.Cfg{}

	cfg.CubeConfig.Customize.CustomStoreHeartbeatDataProcessor = pci

	if cfg.CubeConfig.Prophet.EmbedEtcd.ClientUrls != config.GlobalSystemVariables.GetProphetEmbedEtcdJoinAddr() {
		cfg.CubeConfig.Prophet.EmbedEtcd.Join = config.GlobalSystemVariables.GetProphetEmbedEtcdJoinAddr()
	}

	a, err := driver.NewCubeDriverWithOptions(pebbleDataStorage, aoeDataStorage, &cfg)
	if err != nil {
		logutil.Infof("Create cube driver failed, %v", err)
		os.Exit(CreateCubeExit)
	}
	err = a.Start()
	if err != nil {
		logutil.Infof("Start cube driver failed, %v", err)
		os.Exit(StartCubeExit)
	}

	//put the node info to the computation
	compile.InitAddress(cfg.CubeConfig.ClientAddr)

	c = catalog.NewCatalog(a)
	config.ClusterCatalog = c
	catalogListener.UpdateCatalog(c)
	eng := aoeEngine.New(c)
	pci.SetRemoveEpoch(removeEpoch)

	//hm := config.HostMmu
	//gm := guest.New(1<<40, hm)
	//proc := process.New(gm)
	//{
	//	proc.Id = "0"
	//	proc.Lim.Size = config.GlobalSystemVariables.GetProcessLimitationSize()
	//	proc.Lim.BatchRows = config.GlobalSystemVariables.GetProcessLimitationBatchRows()
	//	proc.Lim.PartitionRows = config.GlobalSystemVariables.GetProcessLimitationPartitionRows()
	//	proc.Lim.BatchSize = config.GlobalSystemVariables.GetProcessLimitationBatchSize()
	//	proc.Refer = make(map[string]uint64)
	//}
	///*	log := logger.New(os.Stderr, "rpc"+strNodeId+": ")
	//	log.SetLevel(logger.WARN)*/
	//srv, err := rpcserver.New(fmt.Sprintf("%s:%d", Host, 20100+NodeId), 1<<30, logutil.GetGlobalLogger())
	//if err != nil {
	//	logutil.Infof("Create rpcserver failed, %v", err)
	//	os.Exit(CreateRPCExit)
	//}
	//hp := handler.New(eng, proc)
	//srv.Register(hp.Process)

	err = waitClusterStartup(a, 300*time.Second, int(cfg.CubeConfig.Prophet.Replication.MaxReplicas), int(cfg.ClusterConfig.PreAllocatedGroupNum))

	if err != nil {
		logutil.Infof("wait cube cluster startup failed, %v", err)
		os.Exit(WaitCubeStartExit)
	}

	//go srv.Run()
	//test storage aoe_storage
	config.StorageEngine = eng

	//test cluster nodes
	config.ClusterNodes = engine.Nodes{}

	createMOServer(pci)

	err = runMOServer()
	if err != nil {
		logutil.Infof("Start MOServer failed, %v", err)
		os.Exit(StartMOExit)
	}
	//registerSignalHandlers()

	waitSignal()
	//srv.Stop()
	serverShutdown(true)
	a.Close()
	aoeDataStorage.Close()
	pebbleDataStorage.Close()

	cleanup()
}

func waitClusterStartup(driver driver.CubeDriver, timeout time.Duration, maxReplicas int, minimalAvailableShard int) error {
	timeoutC := time.After(timeout)
	for {
		select {
		case <-timeoutC:
			return errors.New("wait for available shard timeout")
		default:
			router := driver.RaftStore().GetRouter()
			if router != nil {
				nodeCnt := maxReplicas
				shardCnt := 0
				router.ForeachShards(uint64(pb.AOEGroup), func(shard meta.Shard) bool {
					fmt.Printf("shard %d, peer count is %d\n", shard.ID, len(shard.Replicas))
					shardCnt++
					if len(shard.Replicas) < nodeCnt {
						nodeCnt = len(shard.Replicas)
					}
					return true
				})
				if nodeCnt >= maxReplicas && shardCnt >= minimalAvailableShard {
					kvNodeCnt := maxReplicas
					kvCnt := 0
					router.ForeachShards(uint64(pb.KVGroup), func(shard meta.Shard) bool {
						kvCnt++
						if len(shard.Replicas) < kvNodeCnt {
							kvNodeCnt = len(shard.Replicas)
						}
						return true
					})
					if kvCnt >= 1 && kvNodeCnt >= maxReplicas {
						fmt.Println("ClusterStatus is ok now")
						return nil
					}

				}
			}
			time.Sleep(time.Millisecond * 10)
		}
	}
}
