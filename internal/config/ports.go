// Package config provides shared configuration constants for FG-QiMen.
// Package config 为 FG-QiMen 提供共享配置常量。
//
// This package is inspired by fscan/common/config/constants.go and defines
// the default port lists and port groups for scanning.
//
// 本包借鉴 fscan/common/config/constants.go，定义扫描的默认端口列表和端口组。
package config

// MainPorts is the default port list covering 133 common services.
// MainPorts 是覆盖 133 个常见服务的默认端口列表。
//
// Inspired by fscan's MainPorts, this list includes:
// - Basic services (21-995): FTP, SSH, Telnet, SMTP, DNS, HTTP, POP3, etc.
// - Remote management (1080-1883): SOCKS, MSSQL, Oracle, RDP, MQTT
// - Databases (2049-3690): NFS, Zookeeper, Elasticsearch, MySQL, PostgreSQL
// - Middleware (4369-5986): RabbitMQ, Tomcat, Kibana, Redis, WinRM
// - Message queues (6000-9999): Redis Cluster, Kafka, CouchDB, Jenkins
// - Monitoring/Big Data (10000-61616): Prometheus, Grafana, Hadoop, ActiveMQ
//
// Note: Port 9100 is intentionally excluded (printer RAW port; sending data
// triggers printing, see fscan issue #517).
//
// 借鉴 fscan 的 MainPorts，本列表包含：
// - 基础服务 (21-995)：FTP、SSH、Telnet、SMTP、DNS、HTTP、POP3 等
// - 远程管理 (1080-1883)：SOCKS、MSSQL、Oracle、RDP、MQTT
// - 数据库 (2049-3690)：NFS、Zookeeper、Elasticsearch、MySQL、PostgreSQL
// - 中间件 (4369-5986)：RabbitMQ、Tomcat、Kibana、Redis、WinRM
// - 消息队列 (6000-9999)：Redis Cluster、Kafka、CouchDB、Jenkins
// - 监控/大数据 (10000-61616)：Prometheus、Grafana、Hadoop、ActiveMQ
//
// 注意：9100 端口故意排除（打印机 RAW 端口；发送数据会触发打印，见 fscan issue #517）
var MainPorts = []int{
	// Basic services (21-995) / 基础服务
	21, 22, 23, 25, 53, 80, 81, 88, 110, 111, 135, 139, 143, 161, 389,
	443, 445, 465, 502, 512, 513, 514, 515, 548, 554, 587, 623, 636,
	873, 902, 993, 995,

	// Proxy/Tunnel (1080-1883) / 代理/隧道
	1080, 1099, 1194, 1433, 1434, 1521, 1522, 1525, 1723, 1883,

	// Remote/Database (2049-3690) / 远程/数据库
	2049, 2121, 2181, 2200, 2222, 2375, 2376, 2379, 2380, 3000, 3128,
	3268, 3269, 3306, 3389, 3690,

	// Java/Middleware (4369-5986) / Java/中间件
	4369, 4444, 4848, 5000, 5005, 5044, 5060, 5432, 5601, 5631, 5632,
	5671, 5672, 5900, 5984, 5985, 5986,

	// Cache/Database (6000-6667) / 缓存/数据库
	6000, 6379, 6380, 6443, 6666, 6667,

	// Web/Middleware (7001-9999) / Web/中间件
	// Note: 9100 excluded (printer RAW port) / 注意：9100 排除（打印机 RAW 端口）
	7001, 7002, 7474, 7687, 8000, 8005, 8008, 8009, 8080, 8081, 8086,
	8088, 8089, 8090, 8161, 8180, 8443, 8500, 8834, 8848, 8880, 8883,
	8888, 9000, 9001, 9042, 9080, 9090, 9092, 9093, 9160, 9200, 9300,
	9418, 9443, 9999,

	// Management/Monitoring (10000-11211) / 管理/监控
	10000, 10051, 10250, 10255, 11211,

	// Message Queue/Cluster (15672-27018) / 消息队列/集群
	15672, 22222, 26379, 27017, 27018,

	// Hadoop/Big Data (50000-61616) / Hadoop/大数据
	50000, 50070, 50075, 61613, 61614, 61616,
}

// WebPorts contains 284 common web service ports.
// WebPorts 包含 284 个常见 Web 服务端口。
//
// This list covers HTTP/HTTPS services on non-standard ports commonly used
// by web applications, APIs, admin panels, and middleware.
//
// 本列表覆盖 Web 应用、API、管理面板、中间件常用的非标准 HTTP/HTTPS 端口。
var WebPorts = []int{
	80, 81, 82, 83, 84, 85, 86, 87, 88, 89, 90, 91, 92, 98, 99,
	443, 800, 801, 808, 880, 888, 889,
	1000, 1010, 1080, 1081, 1082, 1099, 1118, 1888,
	2008, 2020, 2100, 2375, 2379,
	3000, 3008, 3128, 3505,
	5555, 6080, 6648, 6868,
	7000, 7001, 7002, 7003, 7004, 7005, 7007, 7008, 7070, 7071, 7074,
	7078, 7080, 7088, 7200, 7680, 7687, 7688, 7777, 7890,
	8000, 8001, 8002, 8003, 8004, 8005, 8006, 8008, 8009, 8010, 8011,
	8012, 8016, 8018, 8020, 8028, 8030, 8038, 8042, 8044, 8046, 8048,
	8053, 8060, 8069, 8070, 8080, 8081, 8082, 8083, 8084, 8085, 8086,
	8087, 8088, 8089, 8090, 8091, 8092, 8093, 8094, 8095, 8096, 8097,
	8098, 8099, 8100, 8101, 8108, 8118, 8161, 8172, 8180, 8181, 8200,
	8222, 8244, 8258, 8280, 8288, 8300, 8360, 8443, 8448, 8484, 8800,
	8834, 8838, 8848, 8858, 8868, 8879, 8880, 8881, 8888, 8899, 8983,
	8989,
	9000, 9001, 9002, 9008, 9010, 9043, 9060, 9080, 9081, 9082, 9083,
	9084, 9085, 9086, 9087, 9088, 9089, 9090, 9091, 9092, 9093, 9094,
	9095, 9096, 9097, 9098, 9099, 9200, 9443, 9448, 9800, 9981, 9986,
	9988, 9998, 9999,
	10000, 10001, 10002, 10004, 10008, 10010, 10051, 10250,
	12018, 12443, 14000, 15672, 15671, 16080,
	18000, 18001, 18002, 18004, 18008, 18080, 18082, 18088, 18090, 18098,
	19001,
	20000, 20720, 20880, 21000, 21501, 21502,
	28018,
}

// DbPorts contains common database ports.
// DbPorts 包含常见数据库端口。
var DbPorts = []int{
	1433,  // MSSQL
	1521,  // Oracle
	3306,  // MySQL
	5432,  // PostgreSQL
	5672,  // RabbitMQ (AMQP)
	5984,  // CouchDB
	6379,  // Redis
	7687,  // Neo4j Bolt
	8086,  // InfluxDB
	9042,  // Cassandra
	9093,  // Kafka
	9160,  // Cassandra Thrift
	9200,  // Elasticsearch
	11211, // Memcached
	26379, // Redis Cluster
	27017, // MongoDB
	27018, // MongoDB shard
	61616, // ActiveMQ
}

// ServicePorts contains common service ports (remote access, file sharing, etc.).
// ServicePorts 包含常见服务端口（远程访问、文件共享等）。
var ServicePorts = []int{
	21,    // FTP
	22,    // SSH
	23,    // Telnet
	25,    // SMTP
	53,    // DNS
	110,   // POP3
	111,   // RPC
	135,   // MSRPC
	139,   // NetBIOS
	143,   // IMAP
	161,   // SNMP
	389,   // LDAP
	445,   // SMB
	465,   // SMTPS
	502,   // Modbus
	512,   // rexec
	513,   // rlogin
	514,   // syslog
	587,   // SMTP (submission)
	623,   // IPMI
	636,   // LDAPS
	873,   // rsync
	993,   // IMAPS
	995,   // POP3S
	1433,  // MSSQL
	1521,  // Oracle
	1883,  // MQTT
	2049,  // NFS
	2181,  // Zookeeper
	2222,  // SSH (alt)
	3306,  // MySQL
	3389,  // RDP
	5432,  // PostgreSQL
	5672,  // AMQP
	5671,  // AMQPS
	5900,  // VNC
	5985,  // WinRM HTTP
	5986,  // WinRM HTTPS
	6379,  // Redis
	8161,  // ActiveMQ Admin
	8443,  // HTTPS (alt)
	8883,  // MQTT over TLS
	9000,  // Various
	9092,  // Kafka
	9093,  // Kafka SSL
	9200,  // Elasticsearch
	10051, // Zabbix agent
	11211, // Memcached
	15672, // RabbitMQ management
	15671, // RabbitMQ management SSL
	27017, // MongoDB
	61616, // ActiveMQ
	61613, // ActiveMQ STOMP
}

// CommonPorts contains the most common ports for quick scans.
// CommonPorts 包含最常见端口，用于快速扫描。
var CommonPorts = []int{
	21,   // FTP
	22,   // SSH
	23,   // Telnet
	25,   // SMTP
	53,   // DNS
	80,   // HTTP
	110,  // POP3
	135,  // MSRPC
	139,  // NetBIOS
	143,  // IMAP
	443,  // HTTPS
	445,  // SMB
	993,  // IMAPS
	995,  // POP3S
	1723, // PPTP
	3389, // RDP
	5060, // SIP
	5985, // WinRM HTTP
	5986, // WinRM HTTPS
}

// PortGroup defines a named group of ports.
// PortGroup 定义命名的端口组。
type PortGroup struct {
	Name        string
	Description string
	Ports       []int
}

// AllPortGroups returns all predefined port groups.
// AllPortGroups 返回所有预定义端口组。
func AllPortGroups() map[string]PortGroup {
	return map[string]PortGroup{
		"main": {
			Name:        "main",
			Description: "133 common service ports (default)",
			Ports:       MainPorts,
		},
		"web": {
			Name:        "web",
			Description: "284 web service ports (HTTP/HTTPS variants)",
			Ports:       WebPorts,
		},
		"db": {
			Name:        "db",
			Description: "Database ports (MySQL, PostgreSQL, MongoDB, Redis, etc.)",
			Ports:       DbPorts,
		},
		"service": {
			Name:        "service",
			Description: "Common services (SSH, FTP, SMB, RDP, etc.)",
			Ports:       ServicePorts,
		},
		"common": {
			Name:        "common",
			Description: "Most common ports for quick scans",
			Ports:       CommonPorts,
		},
	}
}

// GetPortGroup returns the ports for a named group.
// GetPortGroup 返回命名组的端口列表。
func GetPortGroup(name string) ([]int, bool) {
	groups := AllPortGroups()
	group, ok := groups[name]
	if !ok {
		return nil, false
	}
	return group.Ports, true
}
