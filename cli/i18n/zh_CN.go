package i18n

// registerZhCN 注册中文翻译
func registerZhCN() {
	Register("zh_CN", map[string]string{
		// 通用消息
		MsgOK.String():        "✅ OK",
		MsgError.String():     "❌ 错误",
		MsgFailed.String():    "失败",
		MsgSuccess.String():   "成功",
		MsgTimeout.String():   "超时",
		MsgNotFound.String():  "(nil)",
		MsgExists.String():    "✅ 存在",
		MsgNotExists.String(): "❌ 不存在",
		MsgEmpty.String():     "(空)",
		MsgTotal.String():     "总计: %d 个键",
		MsgTime.String():      "(耗时: %v)",
		MsgDistance.String():  "距离: %.2f %s",
		MsgGoodbye.String():   "再见!",

		// CLI 描述
		CliName.String():  "burin-cli",
		CliShort.String(): "Burin 命令行工具",
		CliLong.String():  "Burin 是一个分布式缓存和存储系统的命令行客户端工具\n不带任何命令参数时，将自动进入交互式模式",

		// 命令描述
		CmdVersion.String():     "显示版本信息",
		CmdPing.String():        "测试连接",
		CmdGet.String():         "获取键的值",
		CmdSet.String():         "设置键值对",
		CmdDel.String():         "删除键",
		CmdExists.String():      "检查键是否存在",
		CmdList.String():        "列出键",
		CmdCount.String():       "统计键数量",
		CmdGeo.String():         "地理位置操作",
		CmdGeoAdd.String():      "添加地理位置",
		CmdGeoDist.String():     "计算两点距离",
		CmdGeoRadius.String():   "查询范围内的位置",
		CmdGeoPos.String():      "获取位置坐标",
		CmdCluster.String():     "集群信息",
		CmdHealth.String():      "健康检查",
		CmdUser.String():        "用户管理",
		CmdUserCreate.String():  "创建用户",
		CmdUserDelete.String():  "删除用户",
		CmdUserList.String():    "列出用户",
		CmdUserGrant.String():   "授予权限",
		CmdUserRevoke.String():  "撤销权限",
		CmdInteractive.String(): "进入交互式模式",

		// 参数/标志描述
		FlagAddress.String():  "服务器地址",
		FlagDatabase.String(): "默认数据库",
		FlagTimeout.String():  "操作超时时间",
		FlagUsername.String(): "用户名",
		FlagPassword.String(): "密码",
		FlagTTL.String():      "过期时间 (例如: 10s, 5m, 1h)",
		FlagPrefix.String():   "键前缀",
		FlagLimit.String():    "最大返回数量",
		FlagOffset.String():   "偏移量",

		// 错误消息
		ErrCreateClient.String():    "创建客户端失败: %v",
		ErrConnect.String():         "连接服务器失败: %v",
		ErrDisconnect.String():      "断开连接失败: %v",
		ErrGetFailed.String():       "❌ 获取失败: %v (耗时: %v)",
		ErrSetFailed.String():       "❌ 设置失败: %v (耗时: %v)",
		ErrDelFailed.String():       "❌ 删除失败: %v (耗时: %v)",
		ErrExistsFailed.String():    "❌ 检查失败: %v (耗时: %v)",
		ErrListFailed.String():      "❌ 列出键失败: %v (耗时: %v)",
		ErrCountFailed.String():     "❌ 统计失败: %v (耗时: %v)",
		ErrGeoAddFailed.String():    "❌ 添加失败: %v (耗时: %v)",
		ErrGeoDistFailed.String():   "❌ 计算失败: %v (耗时: %v)",
		ErrGeoRadiusFailed.String(): "❌ 查询失败: %v (耗时: %v)",
		ErrGeoPosFailed.String():    "❌ 获取失败: %v (耗时: %v)",
		ErrClusterFailed.String():   "❌ 获取集群信息失败: %v (耗时: %v)",
		ErrHealthFailed.String():    "❌ 健康检查失败: %v (耗时: %v)",
		ErrPingFailed.String():      "❌ PING 失败: %v (耗时: %v)",

		// 交互式模式消息
		InteractiveConnected.String():   "已连接到 Burin 服务器: %s",
		InteractiveCurrentUser.String(): "当前用户: %s",
		InteractiveCurrentDB.String():   "当前数据库: %s",
		InteractiveHelp.String():        "输入 'help' 查看可用命令，输入 'quit' 或 'exit' 退出",
		InteractivePrompt.String():      "%s@%s[%s]> ",
		InteractiveUsage.String():       "用法: %s",
		InteractiveUnknownCmd.String():  "未知命令: %s",
		InteractiveCurrentNode.String(): "当前节点: %s",

		// 帮助消息
		HelpCache.String():   "缓存操作命令",
		HelpDB.String():      "数据库操作命令",
		HelpGeo.String():     "地理位置操作命令",
		HelpUser.String():    "用户管理命令",
		HelpTx.String():      "事务操作命令",
		HelpNode.String():    "节点管理命令",
		HelpGeneral.String(): "通用帮助信息",

		// 用户管理消息
		UserCreated.String(): "✅ 用户创建成功",
		UserDeleted.String(): "✅ 用户删除成功",
		UserGranted.String(): "✅ 权限授予成功",
		UserRevoked.String(): "✅ 权限撤销成功",
		UserList.String():    "用户列表",

		// 集群消息
		ClusterInfo.String():      "=== 集群信息 ===",
		ClusterLeader.String():    "Leader: %s",
		HealthStatus.String():     "状态: %v",
		HealthNodeID.String():     "节点ID: %v",
		HealthIsLeader.String():   "是否Leader: %v",
		HealthUptime.String():     "运行时间: %v",
		HealthComponents.String(): "组件状态:",

		// 交互模式错误和状态消息
		ErrPrefix.String():            "错误: %v",
		UsagePrefix.String():          "用法: %s",
		UnknownModule.String():        "未知模块: %s",
		UnknownSubcommand.String():    "未知子命令: %s %s",
		AvailableModules.String():     "可用模块: %s",
		AvailableSubcommands.String(): "可用子命令: %s",
		TipMore.String():              "提示: 还有 %d 个键未显示,使用 cache list [prefix] [offset] [limit] 查看更多",
		KeyCount.String():             "键数量: %d",
		ShowingKeys.String():          "显示 %d/%d 个键 (offset=%d, limit=%d):",
		KeyExists.String():            "存在",
		KeyNotExists.String():         "不存在",
		CurrentDatabase.String():      "当前数据库: %s",
		SwitchedToDatabase.String():   "已切换到数据库: %s",
		ReservedDatabase.String():     "错误: database '__burin_*' is reserved for system use",

		// 事务消息
		TxAlreadyActive.String():  "错误: 已存在活跃事务 (ID: %s)",
		TxPleaseCommit.String():   "请先提交或回滚当前事务",
		TxStarted.String():        "事务已开始 (ID: %s)",
		TxCommitted.String():      "事务已提交 (ID: %s)",
		TxRolledBack.String():     "事务已回滚 (ID: %s)",
		TxNoActive.String():       "错误: 没有活跃的事务",
		TxMustBegin.String():      "错误: 没有活跃的事务，请先使用 'tx begin' 开始事务",
		TxID.String():             "事务ID: %s",
		TxStatus.String():         "状态: %v",
		TxNoActiveStatus.String(): "无活跃事务",
	})
}
