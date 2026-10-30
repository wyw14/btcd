// Copyright (c) 2016-2017 The btcsuite developers
// Copyright (c) 2015-2017 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package btcjson

// VersionResult models objects included in the version response.  In the actual
// result, these objects are keyed by the program or API name.
//
// NOTE: This is a btcsuite extension ported from
// github.com/decred/dcrd/dcrjson.
type VersionResult struct {
	VersionString string `json:"versionstring"`
	Major         uint32 `json:"major"`
	Minor         uint32 `json:"minor"`
	Patch         uint32 `json:"patch"`
	Prerelease    string `json:"prerelease"`
	BuildMetadata string `json:"buildmetadata"`
}

// Command status strings returned by the getrpccommands RPC.
const (
	// RPCCommandStatusImplemented is the status for commands that are
	// implemented by the node.
	RPCCommandStatusImplemented = "implemented"

	// RPCCommandStatusWalletRequired is the status for recognized commands
	// which require a connected wallet to be serviced.
	RPCCommandStatusWalletRequired = "wallet_required"

	// RPCCommandStatusUnimplemented is the status for commands which are
	// recognized but not implemented yet.
	RPCCommandStatusUnimplemented = "unimplemented"
)

// Transport strings returned by the getrpccommands RPC.
const (
	// RPCTransportHTTP indicates the capability report was requested over
	// a single-shot HTTP POST JSON-RPC connection.
	RPCTransportHTTP = "http"

	// RPCTransportWebsocket indicates the capability report was requested
	// over a websocket JSON-RPC connection.
	RPCTransportWebsocket = "websocket"
)

// Optional index names returned by the getrpccommands RPC.
const (
	// RPCIndexTx is the full transaction index (--txindex).
	RPCIndexTx = "txindex"

	// RPCIndexAddr is the address index (--addrindex).
	RPCIndexAddr = "addrindex"

	// RPCIndexCF is the compact filter index (--cfindex).
	RPCIndexCF = "cfindex"
)

// RPCIndexStatus models the current state of one of the optional indexes the
// RPC server can use.
type RPCIndexStatus struct {
	// Enabled indicates whether or not the index is currently enabled.
	Enabled bool `json:"enabled"`

	// Detail describes what the index provides and any limitations that
	// apply to it.
	Detail string `json:"detail,omitempty"`
}

// RPCIndexesStatus models the state of all of the optional indexes the RPC
// server can make use of.
type RPCIndexesStatus struct {
	TxIndex   RPCIndexStatus `json:"txindex"`
	AddrIndex RPCIndexStatus `json:"addrindex"`
	CfIndex   RPCIndexStatus `json:"cfindex"`
}

// RPCIndexRequirement models a dependency a single command has on one of the
// optional indexes.
type RPCIndexRequirement struct {
	// Index is the name of the required index (txindex, addrindex, or
	// cfindex).
	Index string `json:"index"`

	// Enabled indicates whether or not the required index is currently
	// enabled.
	Enabled bool `json:"enabled"`

	// Conditional indicates whether the index is only required for some
	// usages of the command, such as a specific optional parameter, rather
	// than for the command as a whole.  When true, the command itself stays
	// usable when the index is disabled.
	Conditional bool `json:"conditional"`

	// Parameter is the command parameter the conditional dependency
	// applies to, when applicable.
	Parameter string `json:"parameter,omitempty"`

	// Detail describes the exact scope and limitation of the dependency.
	Detail string `json:"detail"`
}

// RPCCommandInfo models the capability information for a single recognized
// command.
type RPCCommandInfo struct {
	// Name is the JSON-RPC method name.
	Name string `json:"name"`

	// Status is one of the RPCCommandStatus* constants.
	Status string `json:"status"`

	// HTTP indicates whether the command can be invoked over a standard
	// HTTP POST JSON-RPC connection.
	HTTP bool `json:"http"`

	// Websocket indicates whether the command can be invoked over a
	// websocket JSON-RPC connection.
	Websocket bool `json:"websocket"`

	// Authorized indicates whether the authenticated identity used for the
	// request is allowed to invoke the command.  It mirrors the actual
	// permission determination performed when the command is invoked.
	Authorized bool `json:"authorized"`

	// Requirements describes the optional index requirements of the
	// command, if any.
	Requirements []RPCIndexRequirement `json:"requirements,omitempty"`
}

// RPCNotificationInfo models a server-to-client websocket notification.
// Notifications are not invoked by clients, they are pushed by the server
// after a client subscribes to them with the relevant websocket command.
type RPCNotificationInfo struct {
	// Name is the JSON-RPC notification method name.
	Name string `json:"name"`

	// HTTP indicates whether the notification can be delivered over a
	// standard HTTP POST connection.  Notifications are only delivered
	// over websockets, so this is always false.
	HTTP bool `json:"http"`

	// Websocket indicates whether the notification is delivered over a
	// websocket connection.
	Websocket bool `json:"websocket"`
}

// GetRPCCommandsResult models the result of a successful getrpccommands
// command.
type GetRPCCommandsResult struct {
	// RPCVersion is the JSON-RPC API semantic version supported by the
	// node.
	RPCVersion string `json:"rpcversion"`

	// Transport is the transport (http or websocket) over which the
	// capability report was requested.
	Transport string `json:"transport"`

	// Authenticated indicates the request was made by an authenticated
	// identity.
	Authenticated bool `json:"authenticated"`

	// Admin indicates the authenticated identity has admin (unrestricted)
	// permissions.  When false, the identity is a limited user and only
	// commands with Authorized set to true may be invoked.
	Admin bool `json:"admin"`

	// Indexes describes the state of the optional indexes.
	Indexes RPCIndexesStatus `json:"indexes"`

	// Commands is the list of recognized commands, stably sorted by name.
	Commands []RPCCommandInfo `json:"commands"`

	// Notifications is the list of server-to-client websocket
	// notifications, stably sorted by name.
	Notifications []RPCNotificationInfo `json:"notifications"`
}
