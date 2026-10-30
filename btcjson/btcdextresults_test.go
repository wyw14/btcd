// Copyright (c) 2016-2017 The btcsuite developers
// Copyright (c) 2015-2016 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package btcjson_test

import (
	"encoding/json"
	"testing"

	"github.com/btcsuite/btcd/btcjson"
)

// TestBtcdExtCustomResults ensures any results that have custom marshalling
// work as intended.
// and unmarshal code of results are as expected.
func TestBtcdExtCustomResults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		result   interface{}
		expected string
	}{
		{
			name: "versionresult",
			result: &btcjson.VersionResult{
				VersionString: "1.0.0",
				Major:         1,
				Minor:         0,
				Patch:         0,
				Prerelease:    "pr",
				BuildMetadata: "bm",
			},
			expected: `{"versionstring":"1.0.0","major":1,"minor":0,"patch":0,"prerelease":"pr","buildmetadata":"bm"}`,
		},
		{
			name: "getrpccommandsresult",
			result: &btcjson.GetRPCCommandsResult{
				RPCVersion:    "1.3.0",
				Transport:     btcjson.RPCTransportHTTP,
				Authenticated: true,
				Admin:         false,
				Indexes: btcjson.RPCIndexesStatus{
					TxIndex: btcjson.RPCIndexStatus{
						Enabled: false,
						Detail:  "tx index detail",
					},
					AddrIndex: btcjson.RPCIndexStatus{
						Enabled: true,
						Detail:  "addr index detail",
					},
					CfIndex: btcjson.RPCIndexStatus{
						Enabled: false,
						Detail:  "cf index detail",
					},
				},
				Commands: []btcjson.RPCCommandInfo{
					{
						Name:       "getrawtransaction",
						Status:     btcjson.RPCCommandStatusImplemented,
						HTTP:       true,
						Websocket:  true,
						Authorized: true,
						Requirements: []btcjson.RPCIndexRequirement{
							{
								Index:       btcjson.RPCIndexTx,
								Enabled:     false,
								Conditional: true,
								Detail:      "mempool transactions work without it",
							},
						},
					},
					{
						Name:       "getblock",
						Status:     btcjson.RPCCommandStatusImplemented,
						HTTP:       true,
						Websocket:  true,
						Authorized: true,
					},
				},
				Notifications: []btcjson.RPCNotificationInfo{
					{
						Name:      "blockconnected",
						HTTP:      false,
						Websocket: true,
					},
				},
			},
			expected: `{"rpcversion":"1.3.0","transport":"http","authenticated":true,"admin":false,` +
				`"indexes":{"txindex":{"enabled":false,"detail":"tx index detail"},` +
				`"addrindex":{"enabled":true,"detail":"addr index detail"},` +
				`"cfindex":{"enabled":false,"detail":"cf index detail"}},` +
				`"commands":[{"name":"getrawtransaction","status":"implemented","http":true,"websocket":true,"authorized":true,` +
				`"requirements":[{"index":"txindex","enabled":false,"conditional":true,"detail":"mempool transactions work without it"}]},` +
				`{"name":"getblock","status":"implemented","http":true,"websocket":true,"authorized":true}],` +
				`"notifications":[{"name":"blockconnected","http":false,"websocket":true}]}`,
		},
	}

	t.Logf("Running %d tests", len(tests))
	for i, test := range tests {
		marshalled, err := json.Marshal(test.result)
		if err != nil {
			t.Errorf("Test #%d (%s) unexpected error: %v", i,
				test.name, err)
			continue
		}
		if string(marshalled) != test.expected {
			t.Errorf("Test #%d (%s) unexpected marhsalled data - "+
				"got %s, want %s", i, test.name, marshalled,
				test.expected)
			continue
		}
	}
}
