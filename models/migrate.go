package models

import (
	"log"
	"strings"
)

// migration 是一次幂等的结构变更。
//
// 原来 import.sql 只在首次安装执行一次，之后对它的任何修改（加索引、改字段宽度）
// 对已有部署完全无效。这里补上最小可用的迁移机制：每条变更自带「是否已应用」的
// 判断，重复执行安全，不需要额外的版本表。
type migration struct {
	name string
	// applied 返回 true 表示该变更已经生效，跳过。
	applied func() bool
	stmt    string
}

func migrations() []migration {
	return []migration{
		{
			name:    "message 增加未读查询索引",
			applied: func() bool { return hasIndex("message", "idx_visitor_unread") },
			stmt:    "ALTER TABLE `message` ADD INDEX `idx_visitor_unread` (`visitor_id`,`mes_type`,`status`,`id`)",
		},
		{
			name:    "message 增加 (visitor_id,id) 联合索引",
			applied: func() bool { return hasIndex("message", "idx_visitor_id_id") },
			stmt:    "ALTER TABLE `message` ADD INDEX `idx_visitor_id_id` (`visitor_id`,`id`)",
		},
		{
			name:    "visitor.status 增加索引",
			applied: func() bool { return hasIndex("visitor", "idx_status") },
			stmt:    "ALTER TABLE `visitor` ADD INDEX `idx_status` (`status`)",
		},
		{
			// bcrypt 摘要是 60 字符，varchar(50) 会被静默截断，导致登录永久失败。
			name:    "user.password 扩宽到 100 以容纳 bcrypt",
			applied: func() bool { return columnLength("user", "password") >= 100 },
			stmt:    "ALTER TABLE `user` MODIFY `password` varchar(100) NOT NULL DEFAULT ''",
		},
		{
			name:    "创建 role 表",
			applied: func() bool { return hasTable("role") },
			stmt: "CREATE TABLE `role` (`id` int(11) NOT NULL AUTO_INCREMENT," +
				"`name` varchar(50) NOT NULL DEFAULT ''," +
				"`method` varchar(255) NOT NULL DEFAULT '*'," +
				"`path` varchar(2048) NOT NULL DEFAULT '*'," +
				"PRIMARY KEY (`id`)) ENGINE=InnoDB DEFAULT CHARSET=utf8",
		},
		{
			name:    "创建 user_role 表",
			applied: func() bool { return hasTable("user_role") },
			stmt: "CREATE TABLE `user_role` (`id` int(11) NOT NULL AUTO_INCREMENT," +
				"`user_id` varchar(50) NOT NULL DEFAULT ''," +
				"`role_id` int(11) NOT NULL DEFAULT '0'," +
				"PRIMARY KEY (`id`), KEY `user_id` (`user_id`)) ENGINE=InnoDB DEFAULT CHARSET=utf8",
		},
	}
}

// Migrate 幂等地补齐结构变更，每次启动都可安全执行。
func Migrate() {
	if DB == nil {
		return
	}
	for _, m := range migrations() {
		if m.applied() {
			continue
		}
		if err := DB.Exec(m.stmt).Error; err != nil {
			// 并发启动多个实例时可能重复执行，重复错误忽略即可。
			if isDuplicateErr(err) {
				continue
			}
			log.Printf("migrate %q failed: %v", m.name, err)
			continue
		}
		log.Printf("migrate applied: %s", m.name)
	}
}

func isDuplicateErr(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") || strings.Contains(msg, "exists")
}

func hasTable(table string) bool {
	var count int
	DB.Raw("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?",
		table).Row().Scan(&count)
	return count > 0
}

func hasIndex(table, index string) bool {
	var count int
	DB.Raw("SELECT COUNT(*) FROM information_schema.statistics WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?",
		table, index).Row().Scan(&count)
	return count > 0
}

func columnLength(table, column string) int {
	var length int
	DB.Raw("SELECT IFNULL(character_maximum_length,0) FROM information_schema.columns WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?",
		table, column).Row().Scan(&length)
	return length
}
