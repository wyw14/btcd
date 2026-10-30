// Copyright (c) 2026 The btcsuite developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/btcsuite/btcd/blockchain/indexers"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/stretchr/testify/require"
)

// findCommand returns the capability info for the named command from the
// discovery result and fails the test when it is not present.
func findCommand(t *testing.T, result *btcjson.GetRPCCommandsResult,
	name string) btcjson.RPCCommandInfo {

	t.Helper()

	for _, command := range result.Commands {
		if command.Name == name {
			return command
		}
	}

	t.Fatalf("command %q not found in discovery result", name)
	return btcjson.RPCCommandInfo{}
}

// findRequirement returns the index requirement for the named index from the
// command's requirement list and fails the test when it is not present.
func findRequirement(t *testing.T, info btcjson.RPCCommandInfo,
	index string) btcjson.RPCIndexRequirement {

	t.Helper()

	for _, req := range info.Requirements {
		if req.Index == index {
			return req
		}
	}

	t.Fatalf("requirement %q not found for command %q", index, info.Name)
	return btcjson.RPCIndexRequirement{}
}

// TestRPCCommandsDiscovery verifies the capability discovery report classifies
// every recognized command correctly, reports transport and authorization
// accurately, and describes the optional index dependencies without marking
// partially dependent commands as unavailable.
func TestRPCCommandsDiscovery(t *testing.T) {
	t.Parallel()

	// Use a bare server with no optional indexes enabled.  The discovery
	// query is purely introspective, so it must succeed without chain,
	// mempool, or indexes and must not modify any node state.
	s := &rpcServer{}

	result := s.rpcCommandsDiscovery(true, btcjson.RPCTransportHTTP)

	// Verify the report header.
	require.Equal(t, jsonrpcSemverString, result.RPCVersion)
	require.Equal(t, btcjson.RPCTransportHTTP, result.Transport)
	require.True(t, result.Authenticated)
	require.True(t, result.Admin)

	// All indexes are disabled on the bare server.
	require.False(t, result.Indexes.TxIndex.Enabled)
	require.False(t, result.Indexes.AddrIndex.Enabled)
	require.False(t, result.Indexes.CfIndex.Enabled)
	require.NotEmpty(t, result.Indexes.TxIndex.Detail)
	require.NotEmpty(t, result.Indexes.AddrIndex.Detail)
	require.NotEmpty(t, result.Indexes.CfIndex.Detail)

	// The command list must be stably sorted by name.
	require.True(t, sort.StringsAreSorted(commandNames(result.Commands)))

	// Verify implementation status and transport classification.
	getBlock := findCommand(t, result, "getblock")
	require.Equal(t, btcjson.RPCCommandStatusImplemented, getBlock.Status)
	require.True(t, getBlock.HTTP)
	require.True(t, getBlock.Websocket)
	require.True(t, getBlock.Authorized)
	require.Nil(t, getBlock.Requirements)

	getBalance := findCommand(t, result, "getbalance")
	require.Equal(t, btcjson.RPCCommandStatusWalletRequired,
		getBalance.Status)
	require.True(t, getBalance.HTTP)
	require.True(t, getBalance.Websocket)

	getWork := findCommand(t, result, "getwork")
	require.Equal(t, btcjson.RPCCommandStatusUnimplemented,
		getWork.Status)
	require.True(t, getWork.HTTP)
	require.True(t, getWork.Websocket)

	// Websocket-only commands are not available over HTTP.
	notifyBlocks := findCommand(t, result, "notifyblocks")
	require.Equal(t, btcjson.RPCCommandStatusImplemented,
		notifyBlocks.Status)
	require.False(t, notifyBlocks.HTTP)
	require.True(t, notifyBlocks.Websocket)

	// The websocket authenticate handshake is handled outside of the
	// regular handler maps and can be invoked by any connecting client.
	authenticate := findCommand(t, result, "authenticate")
	require.Equal(t, btcjson.RPCCommandStatusImplemented,
		authenticate.Status)
	require.False(t, authenticate.HTTP)
	require.True(t, authenticate.Websocket)
	require.True(t, authenticate.Authorized)

	// The discovery command itself must be listed as implemented and
	// reachable over both transports.
	discovery := findCommand(t, result, getRPCCommandsMethod)
	require.Equal(t, btcjson.RPCCommandStatusImplemented,
		discovery.Status)
	require.True(t, discovery.HTTP)
	require.True(t, discovery.Websocket)
	require.True(t, discovery.Authorized)

	// Every entry must have a valid status and be reachable over at least
	// one transport.
	for _, command := range result.Commands {
		require.Contains(t, []string{
			btcjson.RPCCommandStatusImplemented,
			btcjson.RPCCommandStatusWalletRequired,
			btcjson.RPCCommandStatusUnimplemented,
		}, command.Status, "unexpected status for %q", command.Name)
		require.True(t, command.HTTP || command.Websocket,
			"command %q is not reachable on any transport",
			command.Name)
	}

	// Index-dependent commands must describe the index state and scope,
	// and the commands must remain implemented and authorized even when
	// the required indexes are disabled.
	getCFilter := findCommand(t, result, "getcfilter")
	require.Equal(t, btcjson.RPCCommandStatusImplemented,
		getCFilter.Status)
	require.True(t, getCFilter.HTTP)
	require.True(t, getCFilter.Authorized)
	cfReq := findRequirement(t, getCFilter, btcjson.RPCIndexCF)
	require.False(t, cfReq.Enabled)
	require.False(t, cfReq.Conditional)
	require.NotEmpty(t, cfReq.Detail)

	getCFilterHeader := findCommand(t, result, "getcfilterheader")
	cfHeaderReq := findRequirement(t, getCFilterHeader,
		btcjson.RPCIndexCF)
	require.False(t, cfHeaderReq.Enabled)
	require.False(t, cfHeaderReq.Conditional)

	// getrawtransaction only needs the tx index for some lookups, so it
	// must carry a conditional requirement rather than being marked
	// unavailable as a whole.
	getRawTx := findCommand(t, result, "getrawtransaction")
	require.Equal(t, btcjson.RPCCommandStatusImplemented,
		getRawTx.Status)
	require.True(t, getRawTx.HTTP)
	require.True(t, getRawTx.Websocket)
	require.True(t, getRawTx.Authorized)
	txReq := findRequirement(t, getRawTx, btcjson.RPCIndexTx)
	require.False(t, txReq.Enabled)
	require.True(t, txReq.Conditional)
	require.NotEmpty(t, txReq.Detail)

	// searchrawtransactions always needs the address index and only needs
	// the transaction index for the vinextra parameter.
	searchRawTxns := findCommand(t, result, "searchrawtransactions")
	require.Equal(t, btcjson.RPCCommandStatusImplemented,
		searchRawTxns.Status)
	require.True(t, searchRawTxns.HTTP)
	require.True(t, searchRawTxns.Authorized)
	addrReq := findRequirement(t, searchRawTxns, btcjson.RPCIndexAddr)
	require.False(t, addrReq.Enabled)
	require.False(t, addrReq.Conditional)
	require.NotEmpty(t, addrReq.Detail)
	txReq = findRequirement(t, searchRawTxns, btcjson.RPCIndexTx)
	require.False(t, txReq.Enabled)
	require.True(t, txReq.Conditional)
	require.Equal(t, "vinextra", txReq.Parameter)
	require.NotEmpty(t, txReq.Detail)

	// Commands which only consult the memory pool must not be reported as
	// requiring any index.
	spendingPrevOut := findCommand(t, result, "gettxspendingprevout")
	require.Nil(t, spendingPrevOut.Requirements)

	// Notifications are websocket-only, stably sorted, and must not be
	// reported as commands.
	require.NotEmpty(t, result.Notifications)
	require.True(t, sort.StringsAreSorted(
		notificationNames(result.Notifications)))
	commandSet := make(map[string]struct{}, len(result.Commands))
	for _, command := range result.Commands {
		commandSet[command.Name] = struct{}{}
	}
	for _, ntfn := range result.Notifications {
		require.False(t, ntfn.HTTP)
		require.True(t, ntfn.Websocket)
		_, isCommand := commandSet[ntfn.Name]
		require.False(t, isCommand, "notification %q must not be "+
			"listed as an invokable command", ntfn.Name)
	}
	require.Contains(t, notificationNames(result.Notifications),
		"blockconnected")
	require.Contains(t, notificationNames(result.Notifications),
		"recvtx")

	// Wallet-only notifications are delivered by a connected wallet, not
	// by the chain server, so they must not be reported as capabilities of
	// this node.
	require.NotContains(t, notificationNames(result.Notifications),
		"accountbalance")
}

// TestRPCCommandsDiscoveryLimitedUser verifies the authorization information
// reported for a limited user is exactly consistent with the permission check
// performed when commands are actually invoked.
func TestRPCCommandsDiscoveryLimitedUser(t *testing.T) {
	t.Parallel()

	s := &rpcServer{}

	for _, transport := range []string{
		btcjson.RPCTransportHTTP,
		btcjson.RPCTransportWebsocket,
	} {
		result := s.rpcCommandsDiscovery(false, transport)

		require.True(t, result.Authenticated)
		require.False(t, result.Admin)
		require.Equal(t, transport, result.Transport)

		for _, command := range result.Commands {
			// Reproduce the exact gate used by processRequest and
			// the websocket request handlers.  The authenticate
			// command is the unauthenticated handshake and is
			// always allowed.
			expectedAuthorized := false
			if _, ok := rpcLimited[command.Name]; ok {
				expectedAuthorized = true
			}
			if _, ok := wsSpecialDispatch[command.Name]; ok {
				expectedAuthorized = true
			}

			require.Equal(t, expectedAuthorized,
				command.Authorized,
				"authorization mismatch for %q over %s",
				command.Name, transport)
		}

		// Spot check representative commands.
		require.True(t,
			findCommand(t, result, "getblock").Authorized)
		require.True(t,
			findCommand(t, result, "help").Authorized)
		require.True(t,
			findCommand(t, result, getRPCCommandsMethod).
				Authorized)
		require.True(t,
			findCommand(t, result, "notifyblocks").Authorized)
		require.False(t,
			findCommand(t, result, "stop").Authorized)
		require.False(t,
			findCommand(t, result, "setgenerate").Authorized)
		require.False(t,
			findCommand(t, result, "stopnotifyblocks").
				Authorized)
	}
}

// TestRPCCommandsDiscoveryIndexesEnabled verifies enabled indexes are reported
// as enabled in both the summary and the per-command requirements.
func TestRPCCommandsDiscoveryIndexesEnabled(t *testing.T) {
	t.Parallel()

	s := &rpcServer{
		cfg: rpcserverConfig{
			TxIndex:   &indexers.TxIndex{},
			AddrIndex: &indexers.AddrIndex{},
			CfIndex:   &indexers.CfIndex{},
		},
	}

	result := s.rpcCommandsDiscovery(true, btcjson.RPCTransportWebsocket)
	require.Equal(t, btcjson.RPCTransportWebsocket, result.Transport)
	require.True(t, result.Indexes.TxIndex.Enabled)
	require.True(t, result.Indexes.AddrIndex.Enabled)
	require.True(t, result.Indexes.CfIndex.Enabled)

	getRawTx := findCommand(t, result, "getrawtransaction")
	require.True(t,
		findRequirement(t, getRawTx, btcjson.RPCIndexTx).Enabled)
	require.True(t,
		findRequirement(t, getRawTx, btcjson.RPCIndexTx).Conditional)

	searchRawTxns := findCommand(t, result, "searchrawtransactions")
	require.True(t,
		findRequirement(t, searchRawTxns, btcjson.RPCIndexAddr).
			Enabled)
	require.True(t,
		findRequirement(t, searchRawTxns, btcjson.RPCIndexTx).
			Enabled)

	getCFilter := findCommand(t, result, "getcfilter")
	require.True(t,
		findRequirement(t, getCFilter, btcjson.RPCIndexCF).Enabled)
}

// TestRPCCommandsDiscoveryDispatch verifies the discovery command is serviced
// through the normal dispatch path with the caller identity and transport and
// that a limited user is permitted to call it.
func TestRPCCommandsDiscoveryDispatch(t *testing.T) {
	t.Parallel()

	s := &rpcServer{}
	closeChan := make(chan struct{})

	// HTTP request using limited credentials.
	cmd := &parsedRPCCmd{
		method: getRPCCommandsMethod,
		cmd:    &btcjson.GetRPCCommandsCmd{},
	}
	res, err := s.standardCmdResult(cmd, closeChan, false)
	require.NoError(t, err)

	result, ok := res.(*btcjson.GetRPCCommandsResult)
	require.True(t, ok)
	require.Equal(t, btcjson.RPCTransportHTTP, result.Transport)
	require.False(t, result.Admin)
	require.True(t, result.Authenticated)

	// A nil close channel identifies the request as a websocket request.
	res, err = s.standardCmdResult(cmd, nil, true)
	require.NoError(t, err)
	result, ok = res.(*btcjson.GetRPCCommandsResult)
	require.True(t, ok)
	require.Equal(t, btcjson.RPCTransportWebsocket, result.Transport)
	require.True(t, result.Admin)
}

// TestRPCCommandsDiscoveryLimitedUserProcessRequest verifies the end to end
// behavior through processRequest: a limited user is allowed to run the
// discovery query, and the commands reported as unauthorized are in fact
// rejected by the permission gate.
func TestRPCCommandsDiscoveryLimitedUserProcessRequest(t *testing.T) {
	t.Parallel()

	s := &rpcServer{}
	closeChan := make(chan struct{})

	newRequest := func(method string) btcjson.Request {
		return btcjson.Request{
			Jsonrpc: btcjson.RpcVersion1,
			ID:      float64(1),
			Method:  method,
			Params:  []json.RawMessage{},
		}
	}

	// The discovery command is available to limited users.
	discoveryReq := newRequest(getRPCCommandsMethod)
	reply := s.processRequest(&discoveryReq, false, closeChan)
	require.NotNil(t, reply)

	var rpcResponse struct {
		Result *btcjson.GetRPCCommandsResult `json:"result"`
		Error  *btcjson.RPCError             `json:"error"`
	}
	require.NoError(t, json.Unmarshal(reply, &rpcResponse))
	require.Nil(t, rpcResponse.Error)
	require.NotNil(t, rpcResponse.Result)
	require.False(t, rpcResponse.Result.Admin)

	// A command the report marks as unauthorized must also be rejected by
	// the actual execution gate.
	stop := findCommand(t, rpcResponse.Result, "stop")
	require.False(t, stop.Authorized)
	stopReq := newRequest("stop")
	rejected := s.processRequest(&stopReq, false, closeChan)
	var rejectedResponse struct {
		Error *btcjson.RPCError `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rejected, &rejectedResponse))
	require.NotNil(t, rejectedResponse.Error)

	// The discovery query is read-only: issuing it repeatedly produces
	// identical results.
	discoveryReq2 := newRequest(getRPCCommandsMethod)
	reply2 := s.processRequest(&discoveryReq2, false, closeChan)
	require.JSONEq(t, string(reply), string(reply2))
}

// TestRPCCommandsHelp ensures the discovery command has help text and shows up
// in the usage overview over both transports.
func TestRPCCommandsHelp(t *testing.T) {
	t.Parallel()

	s := &rpcServer{helpCacher: newHelpCacher()}

	// The command must be included in the one-line usage overview.
	usage, err := s.helpCacher.rpcUsage(true)
	require.NoError(t, err)
	require.Contains(t, usage, getRPCCommandsMethod)

	// The detailed help for the command must generate without errors and
	// without any missing description keys.
	help, err := s.helpCacher.rpcMethodHelp(getRPCCommandsMethod)
	require.NoError(t, err)
	require.Contains(t, help, getRPCCommandsMethod)
}

// commandNames returns the command names from a capability command list.
func commandNames(commands []btcjson.RPCCommandInfo) []string {
	names := make([]string, len(commands))
	for i := range commands {
		names[i] = commands[i].Name
	}
	return names
}

// notificationNames returns the names from a notification list.
func notificationNames(ntfns []btcjson.RPCNotificationInfo) []string {
	names := make([]string, len(ntfns))
	for i := range ntfns {
		names[i] = ntfns[i].Name
	}
	return names
}
