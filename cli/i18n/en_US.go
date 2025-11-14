package i18n

// registerEnUS 注册英文翻译
func registerEnUS() {
	Register("en_US", map[string]string{
		// 通用消息
		MsgOK.String():        "✅ OK",
		MsgError.String():     "❌ Error",
		MsgFailed.String():    "Failed",
		MsgSuccess.String():   "Success",
		MsgTimeout.String():   "Timeout",
		MsgNotFound.String():  "(nil)",
		MsgExists.String():    "✅ Exists",
		MsgNotExists.String(): "❌ Not Exists",
		MsgEmpty.String():     "(empty)",
		MsgTotal.String():     "Total: %d keys",
		MsgTime.String():      "(Time: %v)",
		MsgDistance.String():  "Distance: %.2f %s",
		MsgGoodbye.String():   "Goodbye!",

		// CLI 描述
		CliName.String():  "burin-cli",
		CliShort.String(): "Burin CLI Tool",
		CliLong.String():  "Burin is a command-line client tool for distributed cache and storage system\nWhen no command arguments are provided, it will automatically enter interactive mode",

		// 命令描述
		CmdVersion.String():     "Show version information",
		CmdPing.String():        "Test connection",
		CmdGet.String():         "Get value by key",
		CmdSet.String():         "Set key-value pair",
		CmdDel.String():         "Delete key",
		CmdExists.String():      "Check if key exists",
		CmdList.String():        "List keys",
		CmdCount.String():       "Count keys",
		CmdGeo.String():         "Geographic location operations",
		CmdGeoAdd.String():      "Add geographic location",
		CmdGeoDist.String():     "Calculate distance between two points",
		CmdGeoRadius.String():   "Query locations within radius",
		CmdGeoPos.String():      "Get position coordinates",
		CmdCluster.String():     "Cluster information",
		CmdHealth.String():      "Health check",
		CmdUser.String():        "User management",
		CmdUserCreate.String():  "Create user",
		CmdUserDelete.String():  "Delete user",
		CmdUserList.String():    "List users",
		CmdUserGrant.String():   "Grant permission",
		CmdUserRevoke.String():  "Revoke permission",
		CmdInteractive.String(): "Enter interactive mode",

		// 参数/标志描述
		FlagAddress.String():  "Server address",
		FlagDatabase.String(): "Default database",
		FlagTimeout.String():  "Operation timeout",
		FlagUsername.String(): "Username",
		FlagPassword.String(): "Password",
		FlagTTL.String():      "Expiration time (e.g., 10s, 5m, 1h)",
		FlagPrefix.String():   "Key prefix",
		FlagLimit.String():    "Maximum number of results",
		FlagOffset.String():   "Offset",

		// 错误消息
		ErrCreateClient.String():    "Failed to create client: %v",
		ErrConnect.String():         "Failed to connect to server: %v",
		ErrDisconnect.String():      "Failed to disconnect: %v",
		ErrGetFailed.String():       "❌ Get failed: %v (Time: %v)",
		ErrSetFailed.String():       "❌ Set failed: %v (Time: %v)",
		ErrDelFailed.String():       "❌ Delete failed: %v (Time: %v)",
		ErrExistsFailed.String():    "❌ Check failed: %v (Time: %v)",
		ErrListFailed.String():      "❌ List keys failed: %v (Time: %v)",
		ErrCountFailed.String():     "❌ Count failed: %v (Time: %v)",
		ErrGeoAddFailed.String():    "❌ Add failed: %v (Time: %v)",
		ErrGeoDistFailed.String():   "❌ Calculate failed: %v (Time: %v)",
		ErrGeoRadiusFailed.String(): "❌ Query failed: %v (Time: %v)",
		ErrGeoPosFailed.String():    "❌ Get failed: %v (Time: %v)",
		ErrClusterFailed.String():   "❌ Get cluster info failed: %v (Time: %v)",
		ErrHealthFailed.String():    "❌ Health check failed: %v (Time: %v)",
		ErrPingFailed.String():      "❌ PING failed: %v (Time: %v)",

		// 交互式模式消息
		InteractiveConnected.String():   "Connected to Burin server: %s",
		InteractiveCurrentUser.String(): "Current user: %s",
		InteractiveCurrentDB.String():   "Current database: %s",
		InteractiveHelp.String():        "Type 'help' to see available commands, 'quit' or 'exit' to quit",
		InteractivePrompt.String():      "%s@%s[%s]> ",
		InteractiveUsage.String():       "Usage: %s",
		InteractiveUnknownCmd.String():  "Unknown command: %s",
		InteractiveCurrentNode.String(): "Current node: %s",

		// 帮助消息
		HelpCache.String():   "Cache operation commands",
		HelpDB.String():      "Database operation commands",
		HelpGeo.String():     "Geographic location operation commands",
		HelpUser.String():    "User management commands",
		HelpTx.String():      "Transaction operation commands",
		HelpNode.String():    "Node management commands",
		HelpGeneral.String(): "General help information",

		// 用户管理消息
		UserCreated.String(): "✅ User created successfully",
		UserDeleted.String(): "✅ User deleted successfully",
		UserGranted.String(): "✅ Permission granted successfully",
		UserRevoked.String(): "✅ Permission revoked successfully",
		UserList.String():    "User list",

		// 集群消息
		ClusterInfo.String():      "=== Cluster Information ===",
		ClusterLeader.String():    "Leader: %s",
		HealthStatus.String():     "Status: %v",
		HealthNodeID.String():     "Node ID: %v",
		HealthIsLeader.String():   "Is Leader: %v",
		HealthUptime.String():     "Uptime: %v",
		HealthComponents.String(): "Component Status:",

		// 交互模式错误和状态消息
		ErrPrefix.String():            "Error: %v",
		UsagePrefix.String():          "Usage: %s",
		UnknownModule.String():        "Unknown module: %s",
		UnknownSubcommand.String():    "Unknown subcommand: %s %s",
		AvailableModules.String():     "Available modules: %s",
		AvailableSubcommands.String(): "Available subcommands: %s",
		TipMore.String():              "Tip: %d more keys not shown, use 'cache list [prefix] [offset] [limit]' to see more",
		KeyCount.String():             "Key count: %d",
		ShowingKeys.String():          "Showing %d/%d keys (offset=%d, limit=%d):",
		KeyExists.String():            "Exists",
		KeyNotExists.String():         "Not exists",
		CurrentDatabase.String():      "Current database: %s",
		SwitchedToDatabase.String():   "Switched to database: %s",
		ReservedDatabase.String():     "Error: database '__burin_*' is reserved for system use",

		// 事务消息
		TxAlreadyActive.String():  "Error: Transaction already active (ID: %s)",
		TxPleaseCommit.String():   "Please commit or rollback current transaction first",
		TxStarted.String():        "Transaction started (ID: %s)",
		TxCommitted.String():      "Transaction committed (ID: %s)",
		TxRolledBack.String():     "Transaction rolled back (ID: %s)",
		TxNoActive.String():       "Error: No active transaction",
		TxMustBegin.String():      "Error: No active transaction, please use 'tx begin' to start a transaction first",
		TxID.String():             "Transaction ID: %s",
		TxStatus.String():         "Status: %v",
		TxNoActiveStatus.String(): "No active transaction",
	})
}
