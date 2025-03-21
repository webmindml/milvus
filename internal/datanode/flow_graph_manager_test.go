// Licensed to the LF AI & Data foundation under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information
// regarding copyright ownership. The ASF licenses this file
// to you under the Apache License, Version 2.0 (the
// "License"); you may not use this file except in compliance
// with the License. You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package datanode

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/milvus-io/milvus-proto/go-api/v2/milvuspb"
	"github.com/milvus-io/milvus-proto/go-api/v2/schemapb"
	"github.com/milvus-io/milvus/internal/datanode/broker"
	"github.com/milvus-io/milvus/internal/proto/datapb"
	"github.com/milvus-io/milvus/pkg/util/etcd"
	"github.com/milvus-io/milvus/pkg/util/merr"
)

func TestFlowGraphManager(t *testing.T) {
	ctx, cancel := context.WithCancel(context.TODO())
	defer cancel()

	etcdCli, err := etcd.GetEtcdClient(
		Params.EtcdCfg.UseEmbedEtcd.GetAsBool(),
		Params.EtcdCfg.EtcdUseSSL.GetAsBool(),
		Params.EtcdCfg.Endpoints.GetAsStrings(),
		Params.EtcdCfg.EtcdTLSCert.GetValue(),
		Params.EtcdCfg.EtcdTLSKey.GetValue(),
		Params.EtcdCfg.EtcdTLSCACert.GetValue(),
		Params.EtcdCfg.EtcdTLSMinVersion.GetValue())
	assert.NoError(t, err)
	defer etcdCli.Close()

	node := newIDLEDataNodeMock(ctx, schemapb.DataType_Int64)
	defer node.Stop()
	node.SetEtcdClient(etcdCli)
	err = node.Init()
	require.Nil(t, err)

	meta := NewMetaFactory().GetCollectionMeta(1, "test_collection", schemapb.DataType_Int64)
	broker := broker.NewMockBroker(t)
	broker.EXPECT().ReportTimeTick(mock.Anything, mock.Anything).Return(nil).Maybe()
	broker.EXPECT().SaveBinlogPaths(mock.Anything, mock.Anything).Return(nil).Maybe()
	broker.EXPECT().GetSegmentInfo(mock.Anything, mock.Anything).Return([]*datapb.SegmentInfo{}, nil).Maybe()
	broker.EXPECT().DropVirtualChannel(mock.Anything, mock.Anything).Return(nil, nil).Maybe()
	broker.EXPECT().UpdateChannelCheckpoint(mock.Anything, mock.Anything, mock.Anything).Return(nil).Maybe()
	broker.EXPECT().DescribeCollection(mock.Anything, mock.Anything, mock.Anything).
		Return(&milvuspb.DescribeCollectionResponse{
			Status:         merr.Status(nil),
			CollectionID:   1,
			CollectionName: "test_collection",
			Schema:         meta.GetSchema(),
		}, nil).Maybe()

	node.broker = broker

	fm := newFlowgraphManager()
	defer func() {
		fm.dropAll()
	}()

	t.Run("Test addAndStart", func(t *testing.T) {
		vchanName := "by-dev-rootcoord-dml-test-flowgraphmanager-addAndStart"
		vchan := &datapb.VchannelInfo{
			CollectionID: 1,
			ChannelName:  vchanName,
		}
		require.False(t, fm.exist(vchanName))

		err := fm.addAndStartWithEtcdTickler(node, vchan, nil, genTestTickler())
		assert.NoError(t, err)
		assert.True(t, fm.exist(vchanName))

		fm.dropAll()
	})

	t.Run("Test Release", func(t *testing.T) {
		vchanName := "by-dev-rootcoord-dml-test-flowgraphmanager-Release"
		vchan := &datapb.VchannelInfo{
			CollectionID: 1,
			ChannelName:  vchanName,
		}
		require.False(t, fm.exist(vchanName))

		err := fm.addAndStartWithEtcdTickler(node, vchan, nil, genTestTickler())
		assert.NoError(t, err)
		assert.True(t, fm.exist(vchanName))

		fm.release(vchanName)

		assert.False(t, fm.exist(vchanName))
		fm.dropAll()
	})

	t.Run("Test getFlowgraphService", func(t *testing.T) {
		fg, ok := fm.getFlowgraphService("channel-not-exist")
		assert.False(t, ok)
		assert.Nil(t, fg)
	})
}
