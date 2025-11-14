package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Translator 翻译器接口
type Translator interface {
	T(key string, args ...interface{}) string
}

// Message 翻译消息键
type Message string

// 定义所有消息键常量
const (
	// 通用消息
	MsgOK        Message = "msg.ok"
	MsgError     Message = "msg.error"
	MsgFailed    Message = "msg.failed"
	MsgSuccess   Message = "msg.success"
	MsgTimeout   Message = "msg.timeout"
	MsgNotFound  Message = "msg.not_found"
	MsgExists    Message = "msg.exists"
	MsgNotExists Message = "msg.not_exists"
	MsgEmpty     Message = "msg.empty"
	MsgTotal     Message = "msg.total"
	MsgTime      Message = "msg.time"
	MsgDistance  Message = "msg.distance"
	MsgGoodbye   Message = "msg.goodbye"

	// CLI 描述
	CliName  Message = "cli.name"
	CliShort Message = "cli.short"
	CliLong  Message = "cli.long"

	// 命令描述
	CmdVersion     Message = "cmd.version"
	CmdPing        Message = "cmd.ping"
	CmdGet         Message = "cmd.get"
	CmdSet         Message = "cmd.set"
	CmdDel         Message = "cmd.del"
	CmdExists      Message = "cmd.exists"
	CmdList        Message = "cmd.list"
	CmdCount       Message = "cmd.count"
	CmdGeo         Message = "cmd.geo"
	CmdGeoAdd      Message = "cmd.geo.add"
	CmdGeoDist     Message = "cmd.geo.dist"
	CmdGeoRadius   Message = "cmd.geo.radius"
	CmdGeoPos      Message = "cmd.geo.pos"
	CmdCluster     Message = "cmd.cluster"
	CmdHealth      Message = "cmd.health"
	CmdUser        Message = "cmd.user"
	CmdUserCreate  Message = "cmd.user.create"
	CmdUserDelete  Message = "cmd.user.delete"
	CmdUserList    Message = "cmd.user.list"
	CmdUserGrant   Message = "cmd.user.grant"
	CmdUserRevoke  Message = "cmd.user.revoke"
	CmdInteractive Message = "cmd.interactive"

	// 参数/标志描述
	FlagAddress  Message = "flag.address"
	FlagDatabase Message = "flag.database"
	FlagTimeout  Message = "flag.timeout"
	FlagUsername Message = "flag.username"
	FlagPassword Message = "flag.password"
	FlagTTL      Message = "flag.ttl"
	FlagPrefix   Message = "flag.prefix"
	FlagLimit    Message = "flag.limit"
	FlagOffset   Message = "flag.offset"

	// 错误消息
	ErrCreateClient    Message = "err.create_client"
	ErrConnect         Message = "err.connect"
	ErrDisconnect      Message = "err.disconnect"
	ErrGetFailed       Message = "err.get_failed"
	ErrSetFailed       Message = "err.set_failed"
	ErrDelFailed       Message = "err.del_failed"
	ErrExistsFailed    Message = "err.exists_failed"
	ErrListFailed      Message = "err.list_failed"
	ErrCountFailed     Message = "err.count_failed"
	ErrGeoAddFailed    Message = "err.geo_add_failed"
	ErrGeoDistFailed   Message = "err.geo_dist_failed"
	ErrGeoRadiusFailed Message = "err.geo_radius_failed"
	ErrGeoPosFailed    Message = "err.geo_pos_failed"
	ErrClusterFailed   Message = "err.cluster_failed"
	ErrHealthFailed    Message = "err.health_failed"
	ErrPingFailed      Message = "err.ping_failed"

	// 交互式模式消息
	InteractiveConnected   Message = "interactive.connected"
	InteractiveCurrentUser Message = "interactive.current_user"
	InteractiveCurrentDB   Message = "interactive.current_db"
	InteractiveHelp        Message = "interactive.help"
	InteractivePrompt      Message = "interactive.prompt"
	InteractiveUsage       Message = "interactive.usage"
	InteractiveUnknownCmd  Message = "interactive.unknown_cmd"
	InteractiveCurrentNode Message = "interactive.current_node"

	// 帮助消息
	HelpCache   Message = "help.cache"
	HelpDB      Message = "help.db"
	HelpGeo     Message = "help.geo"
	HelpUser    Message = "help.user"
	HelpTx      Message = "help.tx"
	HelpNode    Message = "help.node"
	HelpGeneral Message = "help.general"

	// 用户管理消息
	UserCreated Message = "user.created"
	UserDeleted Message = "user.deleted"
	UserGranted Message = "user.granted"
	UserRevoked Message = "user.revoked"
	UserList    Message = "user.list"

	// 集群消息
	ClusterInfo      Message = "cluster.info"
	ClusterLeader    Message = "cluster.leader"
	HealthStatus     Message = "health.status"
	HealthNodeID     Message = "health.node_id"
	HealthIsLeader   Message = "health.is_leader"
	HealthUptime     Message = "health.uptime"
	HealthComponents Message = "health.components"

	// 交互模式错误和状态消息
	ErrPrefix            Message = "err.prefix"
	UsagePrefix          Message = "usage.prefix"
	UnknownModule        Message = "unknown.module"
	UnknownSubcommand    Message = "unknown.subcommand"
	AvailableModules     Message = "available.modules"
	AvailableSubcommands Message = "available.subcommands"
	TipMore              Message = "tip.more"
	KeyCount             Message = "key.count"
	ShowingKeys          Message = "showing.keys"
	KeyExists            Message = "key.exists"
	KeyNotExists         Message = "key.not_exists"
	CurrentDatabase      Message = "current.database"
	SwitchedToDatabase   Message = "switched.to.database"
	ReservedDatabase     Message = "reserved.database"

	// 事务消息
	TxAlreadyActive  Message = "tx.already_active"
	TxPleaseCommit   Message = "tx.please_commit"
	TxStarted        Message = "tx.started"
	TxCommitted      Message = "tx.committed"
	TxRolledBack     Message = "tx.rolled_back"
	TxNoActive       Message = "tx.no_active"
	TxMustBegin      Message = "tx.must_begin"
	TxID             Message = "tx.id"
	TxStatus         Message = "tx.status"
	TxNoActiveStatus Message = "tx.no_active_status"
)

// String 返回消息键字符串
func (m Message) String() string {
	return string(m)
}

var (
	currentLang  = "zh_CN"
	translations = make(map[string]map[string]string)
	currentTrans Translator
)

// translator 实现
type translator struct {
	lang  string
	trans map[string]string
}

func (t *translator) T(key string, args ...interface{}) string {
	if t.trans == nil {
		return key
	}

	template, ok := t.trans[key]
	if !ok {
		return key
	}

	if len(args) > 0 {
		return fmt.Sprintf(template, args...)
	}
	return template
}

// Init 初始化i18n系统
func Init(lang string) {
	// 注册所有语言
	registerZhCN()
	registerEnUS()

	// 如果没有指定语言，尝试从环境变量获取
	if lang == "" {
		lang = detectLanguage()
	}

	// 设置当前语言
	SetLanguage(lang)
}

// detectLanguage 从环境变量检测语言
func detectLanguage() string {
	// 尝试 LANG 环境变量
	if lang := os.Getenv("LANG"); lang != "" {
		// LANG 格式通常是 zh_CN.UTF-8 或 en_US.UTF-8
		parts := strings.Split(lang, ".")
		if len(parts) > 0 {
			locale := parts[0]
			// 转换为我们的格式
			if strings.HasPrefix(strings.ToLower(locale), "zh") {
				return "zh_CN"
			}
			if strings.HasPrefix(strings.ToLower(locale), "en") {
				return "en_US"
			}
		}
	}

	// 默认返回中文
	return "zh_CN"
}

// SetLanguage 设置当前语言
func SetLanguage(lang string) {
	// 规范化语言代码
	lang = strings.ToLower(lang)
	switch lang {
	case "zh", "zh_cn", "zh-cn", "chinese", "cn":
		lang = "zh_CN"
	case "en", "en_us", "en-us", "english", "us":
		lang = "en_US"
	default:
		lang = "zh_CN" // 默认中文
	}

	currentLang = lang
	if trans, ok := translations[lang]; ok {
		currentTrans = &translator{lang: lang, trans: trans}
	} else {
		// 回退到中文
		currentTrans = &translator{lang: "zh_CN", trans: translations["zh_CN"]}
	}
}

// GetLanguage 获取当前语言
func GetLanguage() string {
	return currentLang
}

// T 翻译消息
func T(key Message, args ...interface{}) string {
	if currentTrans == nil {
		return key.String()
	}
	return currentTrans.T(key.String(), args...)
}

// Register 注册语言翻译
func Register(lang string, trans map[string]string) {
	translations[lang] = trans
}

// GetAvailableLanguages 获取所有可用语言
func GetAvailableLanguages() []string {
	langs := make([]string, 0, len(translations))
	for lang := range translations {
		langs = append(langs, lang)
	}
	return langs
}
