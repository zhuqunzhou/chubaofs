// Copyright 2018 The CFS Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied. See the License for the specific language governing
// permissions and limitations under the License.

package master

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chubaofs/chubaofs/master/mocktest"
	"github.com/chubaofs/chubaofs/proto"
	"github.com/chubaofs/chubaofs/util/config"
	"github.com/chubaofs/chubaofs/util/log"
)

const (
	hostAddr          = "http://127.0.0.1:8080"
	ConfigKeyLogDir   = "logDir"
	ConfigKeyLogLevel = "logLevel"
	mds1Addr          = "127.0.0.1:9101"
	mds2Addr          = "127.0.0.1:9102"
	mds3Addr          = "127.0.0.1:9103"
	mds4Addr          = "127.0.0.1:9104"
	mds5Addr          = "127.0.0.1:9105"

	mms1Addr      = "127.0.0.1:8101"
	mms2Addr      = "127.0.0.1:8102"
	mms3Addr      = "127.0.0.1:8103"
	mms4Addr      = "127.0.0.1:8104"
	mms5Addr      = "127.0.0.1:8105"
	mms6Addr      = "127.0.0.1:8106"
	commonVolName = "commonVol"
	testZone1     = "zone1"
	testZone2     = "zone2"
)

var server = createDefaultMasterServerForTest()
var commonVol *Vol

func createDefaultMasterServerForTest() *Server {
	cfgJSON := `{
		"role": "master",
		"ip": "127.0.0.1",
		"listen": "8080",
		"prof":"10088",
		"id":"1",
		"peers": "1:127.0.0.1:8080",
		"retainLogs":"20000",
		"tickInterval":500,
		"electionTick":6,
		"logDir": "/tmp/chubaofs/Logs",
		"logLevel":"DEBUG",
		"walDir":"/tmp/chubaofs/raft",
		"storeDir":"/tmp/chubaofs/rocksdbstore",
		"clusterName":"chubaofs"
	}`
	testServer, err := createMasterServer(cfgJSON)
	if err != nil {
		panic(err)
	}
	//add data node
	addDataServer(mds1Addr, testZone1)
	addDataServer(mds2Addr, testZone1)
	addDataServer(mds3Addr, testZone2)
	addDataServer(mds4Addr, testZone2)
	addDataServer(mds5Addr, testZone2)
	// add meta node
	addMetaServer(mms1Addr, testZone1)
	addMetaServer(mms2Addr, testZone1)
	addMetaServer(mms3Addr, testZone2)
	addMetaServer(mms4Addr, testZone2)
	addMetaServer(mms5Addr, testZone2)
	time.Sleep(5 * time.Second)
	testServer.cluster.checkDataNodeHeartbeat()
	testServer.cluster.checkMetaNodeHeartbeat()
	time.Sleep(5 * time.Second)
	testServer.cluster.scheduleToUpdateStatInfo()
	vol, err := testServer.cluster.createVol(commonVolName, "cfs", "", 3, 3, 3, 100, false, false, false, false)
	if err != nil {
		panic(err)
	}
	vol, err = testServer.cluster.getVol(commonVolName)
	if err != nil {
		panic(err)
	}
	commonVol = vol
	fmt.Printf("vol[%v] has created\n", commonVol.Name)
	return testServer
}

func createMasterServer(cfgJSON string) (server *Server, err error) {
	cfg := config.LoadConfigString(cfgJSON)
	server = NewServer()
	useConnPool = false
	logDir := cfg.GetString(ConfigKeyLogDir)
	walDir := cfg.GetString(WalDir)
	storeDir := cfg.GetString(StoreDir)
	profPort := cfg.GetString("prof")
	os.RemoveAll(logDir)
	os.RemoveAll(walDir)
	os.RemoveAll(storeDir)
	os.Mkdir(walDir, 0755)
	os.Mkdir(storeDir, 0755)
	logLevel := cfg.GetString(ConfigKeyLogLevel)
	var level log.Level
	switch strings.ToLower(logLevel) {
	case "debug":
		level = log.DebugLevel
	case "info":
		level = log.InfoLevel
	case "warn":
		level = log.WarnLevel
	case "error":
		level = log.ErrorLevel
	default:
		level = log.ErrorLevel
	}
	if _, err = log.InitLog(logDir, "master", level, nil); err != nil {
		fmt.Println("Fatal: failed to start the chubaofs daemon - ", err)
		return
	}
	if profPort != "" {
		go func() {
			err := http.ListenAndServe(fmt.Sprintf(":%v", profPort), nil)
			if err != nil {
				panic(fmt.Sprintf("cannot listen pprof %v err %v", profPort, err.Error()))
			}
		}()
	}
	if err = server.Start(cfg); err != nil {
		return
	}
	time.Sleep(5 * time.Second)
	fmt.Println(server.config.peerAddrs, server.leaderInfo.addr)
	return server, nil
}

func addDataServer(addr, zoneName string) {
	mds := mocktest.NewMockDataServer(addr, zoneName)
	mds.Start()
}

func addMetaServer(addr, zoneName string) {
	mms := mocktest.NewMockMetaServer(addr, zoneName)
	mms.Start()
}

func TestSetMetaNodeThreshold(t *testing.T) {
	threshold := 0.5
	reqURL := fmt.Sprintf("%v%v?threshold=%v", hostAddr, proto.AdminSetMetaNodeThreshold, threshold)
	fmt.Println(reqURL)
	process(reqURL, t)
	if server.cluster.cfg.MetaNodeThreshold != float32(threshold) {
		t.Errorf("set metanode threshold to %v failed", threshold)
		return
	}
}

func TestSetDisableAutoAlloc(t *testing.T) {
	enable := true
	reqURL := fmt.Sprintf("%v%v?enable=%v", hostAddr, proto.AdminClusterFreeze, enable)
	fmt.Println(reqURL)
	process(reqURL, t)
	if server.cluster.DisableAutoAllocate != enable {
		t.Errorf("set disableAutoAlloc to %v failed", enable)
		return
	}
	server.cluster.DisableAutoAllocate = false
}

func TestGetCluster(t *testing.T) {
	reqURL := fmt.Sprintf("%v%v", hostAddr, proto.AdminGetCluster)
	fmt.Println(reqURL)
	process(reqURL, t)
}

func TestGetIpAndClusterName(t *testing.T) {
	reqURL := fmt.Sprintf("%v%v", hostAddr, proto.AdminGetIP)
	fmt.Println(reqURL)
	process(reqURL, t)
}

func process(reqURL string, t *testing.T) (reply *proto.HTTPReply) {
	resp, err := http.Get(reqURL)
	if err != nil {
		t.Errorf("err is %v", err)
		return
	}
	fmt.Println(resp.StatusCode)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("err is %v", err)
		return
	}
	fmt.Println(string(body))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status code[%v]", resp.StatusCode)
		return
	}
	reply = &proto.HTTPReply{}
	if err = json.Unmarshal(body, reply); err != nil {
		t.Error(err)
		return
	}
	if reply.Code != 0 {
		t.Errorf("failed,msg[%v],data[%v]", reply.Msg, reply.Data)
		return
	}
	return
}

func TestDisk(t *testing.T) {
	addr := mds5Addr
	disk := "/cfs"
	decommissionDisk(addr, disk, t)
}

func decommissionDisk(addr, path string, t *testing.T) {
	reqURL := fmt.Sprintf("%v%v?addr=%v&disk=%v",
		hostAddr, proto.DecommissionDisk, addr, path)
	fmt.Println(reqURL)
	resp, err := http.Get(reqURL)
	if err != nil {
		t.Errorf("err is %v", err)
		return
	}
	fmt.Println(resp.StatusCode)
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		t.Errorf("err is %v", err)
		return
	}
	fmt.Println(string(body))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status code[%v]", resp.StatusCode)
		return
	}
	reply := &proto.HTTPReply{}
	if err = json.Unmarshal(body, reply); err != nil {
		t.Error(err)
		return
	}
	server.cluster.checkDataNodeHeartbeat()
	time.Sleep(5 * time.Second)
	server.cluster.checkDiskRecoveryProgress()
}

func TestMarkDeleteVol(t *testing.T) {
	name := "delVol"
	createVol(name, t)
	reqURL := fmt.Sprintf("%v%v?name=%v&authKey=%v", hostAddr, proto.AdminDeleteVol, name, buildAuthKey("cfs"))
	process(reqURL, t)
}

func TestUpdateVol(t *testing.T) {
	capacity := 2000
	reqURL := fmt.Sprintf("%v%v?name=%v&capacity=%v&authKey=%v",
		hostAddr, proto.AdminUpdateVol, commonVol.Name, capacity, buildAuthKey("cfs"))
	process(reqURL, t)
	vol, err := server.cluster.getVol(commonVolName)
	if err != nil {
		t.Error(err)
		return
	}
	if vol.FollowerRead != false {
		t.Errorf("expect FollowerRead is false, but is %v", vol.FollowerRead)
		return
	}

	reqURL = fmt.Sprintf("%v%v?name=%v&capacity=%v&authKey=%v&followerRead=true",
		hostAddr, proto.AdminUpdateVol, commonVol.Name, capacity, buildAuthKey("cfs"))
	process(reqURL, t)
	if vol.FollowerRead != true {
		t.Errorf("expect FollowerRead is true, but is %v", vol.FollowerRead)
		return
	}

}
func buildAuthKey(owner string) string {
	h := md5.New()
	h.Write([]byte(owner))
	cipherStr := h.Sum(nil)
	return hex.EncodeToString(cipherStr)
}

func TestGetVolSimpleInfo(t *testing.T) {
	reqURL := fmt.Sprintf("%v%v?name=%v", hostAddr, proto.AdminGetVol, commonVol.Name)
	process(reqURL, t)
}

func TestCreateVol(t *testing.T) {
	name := "test_create_vol"
	reqURL := fmt.Sprintf("%v%v?name=%v&replicas=3&type=extent&capacity=100&owner=cfs", hostAddr, proto.AdminCreateVol, name)
	fmt.Println(reqURL)
	process(reqURL, t)
}

func TestCreateMetaPartition(t *testing.T) {
	server.cluster.checkMetaNodeHeartbeat()
	time.Sleep(5 * time.Second)
	commonVol.checkMetaPartitions(server.cluster)
	createMetaPartition(commonVol, t)
}

func TestCreateDataPartition(t *testing.T) {
	reqURL := fmt.Sprintf("%v%v?count=2&name=%v&type=extent",
		hostAddr, proto.AdminCreateDataPartition, commonVol.Name)
	process(reqURL, t)
}

func TestGetDataPartition(t *testing.T) {
	if len(commonVol.dataPartitions.partitions) == 0 {
		t.Errorf("no data partitions")
		return
	}
	partition := commonVol.dataPartitions.partitions[0]
	reqURL := fmt.Sprintf("%v%v?id=%v", hostAddr, proto.AdminGetDataPartition, partition.PartitionID)
	process(reqURL, t)

	reqURL = fmt.Sprintf("%v%v?id=%v&name=%v", hostAddr, proto.AdminGetDataPartition, partition.PartitionID, partition.VolName)
	process(reqURL, t)
}

func TestLoadDataPartition(t *testing.T) {
	if len(commonVol.dataPartitions.partitions) == 0 {
		t.Errorf("no data partitions")
		return
	}
	partition := commonVol.dataPartitions.partitions[0]
	reqURL := fmt.Sprintf("%v%v?id=%v&name=%v",
		hostAddr, proto.AdminLoadDataPartition, partition.PartitionID, commonVol.Name)
	process(reqURL, t)
}

func TestDataPartitionDecommission(t *testing.T) {
	if len(commonVol.dataPartitions.partitions) == 0 {
		t.Errorf("no data partitions")
		return
	}
	server.cluster.checkDataNodeHeartbeat()
	time.Sleep(5 * time.Second)
	partition := commonVol.dataPartitions.partitions[0]
	offlineAddr := partition.Hosts[0]
	reqURL := fmt.Sprintf("%v%v?name=%v&id=%v&addr=%v",
		hostAddr, proto.AdminDecommissionDataPartition, commonVol.Name, partition.PartitionID, offlineAddr)
	process(reqURL, t)
	if contains(partition.Hosts, offlineAddr) {
		t.Errorf("offlineAddr[%v],hosts[%v]", offlineAddr, partition.Hosts)
		return
	}
	partition.isRecover = false
}

//func TestGetAllVols(t *testing.T) {
//	reqURL := fmt.Sprintf("%v%v", hostAddr, proto.GetALLVols)
//	process(reqURL, t)
//}
//
func TestGetMetaPartitions(t *testing.T) {
	reqURL := fmt.Sprintf("%v%v?name=%v", hostAddr, proto.ClientMetaPartitions, commonVolName)
	process(reqURL, t)
}

func TestGetDataPartitions(t *testing.T) {
	reqURL := fmt.Sprintf("%v%v?name=%v", hostAddr, proto.ClientDataPartitions, commonVolName)
	process(reqURL, t)
}

func TestGetTopo(t *testing.T) {
	reqURL := fmt.Sprintf("%v%v", hostAddr, proto.GetTopologyView)
	process(reqURL, t)
}

func TestGetMetaNode(t *testing.T) {
	reqURL := fmt.Sprintf("%v%v?addr=%v", hostAddr, proto.GetMetaNode, mms1Addr)
	process(reqURL, t)
}

func TestAddDataReplica(t *testing.T) {
	partition := commonVol.dataPartitions.partitions[0]
	dsAddr := "127.0.0.1:9106"
	addDataServer(dsAddr, "zone2")
	reqURL := fmt.Sprintf("%v%v?id=%v&addr=%v", hostAddr, proto.AdminAddDataReplica, partition.PartitionID, dsAddr)
	process(reqURL, t)
	partition.RLock()
	if !contains(partition.Hosts, dsAddr) {
		t.Errorf("hosts[%v] should contains dsAddr[%v]", partition.Hosts, dsAddr)
		partition.RUnlock()
		return
	}
	partition.RUnlock()
}

func TestRemoveDataReplica(t *testing.T) {
	partition := commonVol.dataPartitions.partitions[0]
	dsAddr := "127.0.0.1:9106"
	reqURL := fmt.Sprintf("%v%v?id=%v&addr=%v", hostAddr, proto.AdminDeleteDataReplica, partition.PartitionID, dsAddr)
	process(reqURL, t)
	partition.RLock()
	if contains(partition.Hosts, dsAddr) {
		t.Errorf("hosts[%v] should contains dsAddr[%v]", partition.Hosts, dsAddr)
		partition.RUnlock()
		return
	}
	partition.isRecover = false
	partition.RUnlock()
}

func TestAddMetaReplica(t *testing.T) {
	maxPartitionID := commonVol.maxPartitionID()
	partition := commonVol.MetaPartitions[maxPartitionID]
	if partition == nil {
		t.Error("no meta partition")
		return
	}
	msAddr := "127.0.0.1:8009"
	addMetaServer(msAddr, testZone2)
	server.cluster.checkMetaNodeHeartbeat()
	time.Sleep(2 * time.Second)
	reqURL := fmt.Sprintf("%v%v?id=%v&addr=%v", hostAddr, proto.AdminAddMetaReplica, partition.PartitionID, msAddr)
	process(reqURL, t)
	partition.RLock()
	if !contains(partition.Hosts, msAddr) {
		t.Errorf("hosts[%v] should contains dsAddr[%v]", partition.Hosts, msAddr)
		partition.RUnlock()
		return
	}
	partition.RUnlock()
}

func TestRemoveMetaReplica(t *testing.T) {
	maxPartitionID := commonVol.maxPartitionID()
	partition := commonVol.MetaPartitions[maxPartitionID]
	if partition == nil {
		t.Error("no meta partition")
		return
	}
	msAddr := "127.0.0.1:8009"
	reqURL := fmt.Sprintf("%v%v?id=%v&addr=%v", hostAddr, proto.AdminDeleteMetaReplica, partition.PartitionID, msAddr)
	process(reqURL, t)
	partition.RLock()
	if contains(partition.Hosts, msAddr) {
		t.Errorf("hosts[%v] should contains dsAddr[%v]", partition.Hosts, msAddr)
		partition.RUnlock()
		return
	}
	partition.RUnlock()
}

func TestAddToken(t *testing.T) {
	reqUrl := fmt.Sprintf("%v%v?name=%v&tokenType=%v&authKey=%v",
		hostAddr, proto.TokenAddURI, commonVol.Name, proto.ReadWriteToken, buildAuthKey("cfs"))
	fmt.Println(reqUrl)
	process(reqUrl, t)
}

func TestDelToken(t *testing.T) {
	for _, token := range commonVol.tokens {
		reqUrl := fmt.Sprintf("%v%v?name=%v&token=%v&authKey=%v",
			hostAddr, proto.TokenDelURI, commonVol.Name, token.Value, buildAuthKey("cfs"))
		fmt.Println(reqUrl)
		process(reqUrl, t)
		_, ok := commonVol.tokens[token.Value]
		if ok {
			t.Errorf("delete token[%v] failed\n", token.Value)
			return
		}

		reqUrl = fmt.Sprintf("%v%v?name=%v&tokenType=%v&authKey=%v",
			hostAddr, proto.TokenAddURI, commonVol.Name, token.TokenType, buildAuthKey("cfs"))
		fmt.Println(reqUrl)
		process(reqUrl, t)
	}
}

func TestUpdateToken(t *testing.T) {
	var tokenType int8
	for _, token := range commonVol.tokens {
		if token.TokenType == proto.ReadWriteToken {
			tokenType = proto.ReadOnlyToken
		} else {
			tokenType = proto.ReadWriteToken
		}

		reqUrl := fmt.Sprintf("%v%v?name=%v&token=%v&tokenType=%v&authKey=%v",
			hostAddr, proto.TokenUpdateURI, commonVol.Name, token.Value, tokenType, buildAuthKey("cfs"))
		fmt.Println(reqUrl)
		process(reqUrl, t)
		token := commonVol.tokens[token.Value]
		if token.TokenType != tokenType {
			t.Errorf("expect tokenType[%v],real tokenType[%v]\n", tokenType, token.TokenType)
			return
		}
	}
}

func TestGetToken(t *testing.T) {
	for _, token := range commonVol.tokens {
		reqUrl := fmt.Sprintf("%v%v?name=%v&token=%v",
			hostAddr, proto.TokenGetURI, commonVol.Name, token.Value)
		fmt.Println(reqUrl)
		process(reqUrl, t)
	}
}

func TestClusterStat(t *testing.T) {
	reqUrl := fmt.Sprintf("%v%v", hostAddr, proto.AdminClusterStat)
	fmt.Println(reqUrl)
	process(reqUrl, t)
}
