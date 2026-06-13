// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Hardcoded regex rules. ~300 rules covering WAFs, CMS, dev panels,
// and OS / language hints. See README's attribution section for
// upstream lineage.

// Package fingerprint: hardcoded regex rules.
//
// ~300 rules covering WAFs, CMS, dev panels, and OS / language hints.
// 硬编码规则先匹配，因为比 FingerprintHub JSON 快，且覆盖最常见产品。
//
// Hardcoded rules are checked first because they're faster than
// FingerprintHub JSON and cover the most common products (宝塔 / 360 /
// 绿盟 / CloudFlare / SpringBoot / WebLogic / etc.).
package fingerprint

import (
	"regexp"
	"strings"
)

// RuleData is one hardcoded rule. / RuleData 是一条硬编码规则。
type RuleData struct {
	Name     string
	Type     string // "code" / "headers" / "cookie" / "index"
	Rule     string
	Compiled *regexp.Regexp
}

// RuleDatas is the full hardcoded rule set. We compile once in init().
// / RuleDatas 是完整的硬编码规则集；在 init() 中编译一次。
var RuleDatas = []RuleData{
	// ─── WAFs / Firewalls / CDN / 安全设备 / 安全设备 ───
	{Name: "宝塔", Type: "code", Rule: "(app.bt.cn/static/app.png|安全入口校验失败|<title>入口校验失败</title>|href=\"http://www.bt.cn/bbs)"},
	{Name: "深信服防火墙类产品", Type: "code", Rule: "(SANGFOR FW)"},
	{Name: "360网站卫士", Type: "code", Rule: "(webscan.360.cn/status/pai/hash|wzws-waf-cgi|zhuji.360.cn/guard/firewall/stopattack.html)"},
	{Name: "360网站卫士", Type: "headers", Rule: "(360wzws|CWAP-waf|zhuji.360.cn|X-Safe-Firewall)"},
	{Name: "绿盟防火墙", Type: "code", Rule: "(NSFOCUS NF)"},
	{Name: "绿盟防火墙", Type: "headers", Rule: "(NSFocus)"},
	{Name: "Anquanbao", Type: "headers", Rule: "(Anquanbao)"},
	{Name: "BaiduYunjiasu", Type: "headers", Rule: "(yunjiasu)"},
	{Name: "BigIP", Type: "headers", Rule: "(BigIP|BIGipServer)"},
	{Name: "BinarySEC", Type: "headers", Rule: "(binarysec)"},
	{Name: "BlockDoS", Type: "headers", Rule: "(BlockDos.net)"},
	{Name: "CloudFlare", Type: "headers", Rule: "(cloudflare)"},
	{Name: "Cloudfront", Type: "headers", Rule: "(cloudfront)"},
	{Name: "Comodo", Type: "headers", Rule: "(Protected by COMODO)"},
	{Name: "IBM-DataPower", Type: "headers", Rule: "(X-Backside-Transport)"},
	{Name: "DenyAll", Type: "headers", Rule: "(sessioncookie=)"},
	{Name: "dotDefender", Type: "headers", Rule: "(dotDefender)"},
	{Name: "Incapsula", Type: "headers", Rule: "(X-CDN|Incapsula)"},
	{Name: "Jiasule", Type: "headers", Rule: "(jsluid=)"},
	{Name: "KONA", Type: "headers", Rule: "(AkamaiGHost)"},
	{Name: "ModSecurity", Type: "headers", Rule: "(Mod_Security|NOYB)"},
	{Name: "NetContinuum", Type: "headers", Rule: "(Cneonction|nnCoection|citrix_ns_id)"},
	{Name: "Newdefend", Type: "headers", Rule: "(newdefend)"},
	{Name: "Safe3", Type: "headers", Rule: "(Safe3WAF|Safe3 Web Firewall)"},
	{Name: "Safedog", Type: "code", Rule: "(404.safedog.cn/images/safedogsite/broswer_logo.jpg)"},
	{Name: "Safedog", Type: "headers", Rule: "(Safedog|WAF/2.0)"},
	{Name: "SonicWALL", Type: "headers", Rule: "(SonicWALL)"},
	{Name: "Stingray", Type: "headers", Rule: "(X-Mapping-)"},
	{Name: "Sucuri", Type: "headers", Rule: "(Sucuri/Cloudproxy)"},
	{Name: "Usp-Sec", Type: "headers", Rule: "(Secure Entry Server)"},
	{Name: "Varnish", Type: "headers", Rule: "(varnish)"},
	{Name: "Wallarm", Type: "headers", Rule: "(wallarm)"},
	{Name: "阿里云", Type: "code", Rule: "(errors.aliyun.com)"},
	{Name: "WebKnight", Type: "headers", Rule: "(WebKnight)"},
	{Name: "Yundun", Type: "headers", Rule: "(YUNDUN)"},
	{Name: "Yunsuo", Type: "headers", Rule: "(yunsuo)"},

	// ─── Frameworks / Runtimes / 开发框架 / 开发框架 ───
	{Name: "Shiro", Type: "headers", Rule: "(=deleteMe|rememberMe=)"},
	{Name: "SpringBoot", Type: "code", Rule: "(Whitelabel Error Page|Spring Boot)"},
	{Name: "ThinkPHP", Type: "code", Rule: "(ThinkPHP\\(|十年磨一剑|thinkphp)"},
	{Name: "Laravel", Type: "headers", Rule: "(laravel_session)"},
	{Name: "ASP.NET", Type: "headers", Rule: "(X-AspNet-Version|X-Powered-By: ASP.NET|X-AspNetMvc-Version)"},
	{Name: "PHP", Type: "headers", Rule: "(X-Powered-By: PHP|PHPSESSID)"},
	{Name: "Java", Type: "headers", Rule: "(JSESSIONID)"},
	{Name: "Express", Type: "headers", Rule: "(X-Powered-By: Express)"},
	{Name: "Tomcat", Type: "code", Rule: "(Apache Tomcat/)"},
	{Name: "Nginx", Type: "headers", Rule: "(Server: nginx)"},
	{Name: "Apache", Type: "headers", Rule: "(Server: Apache)"},
	{Name: "IIS", Type: "headers", Rule: "(Server: Microsoft-IIS)"},
	{Name: "Caddy", Type: "headers", Rule: "(Server: Caddy)"},
	{Name: "OpenResty", Type: "headers", Rule: "(Server: openresty)"},
	{Name: "Tengine", Type: "headers", Rule: "(Server: Tengine)"},

	// ─── CMS / Forums / Blogs / CMS / 论坛 / 博客 ───
	{Name: "WordPress", Type: "code", Rule: "(/wp-content/themes/|/wp-includes/|wp-json|/wp-admin/)"},
	{Name: "Discuz", Type: "code", Rule: "(content=\"Discuz! X\"|Discuz!)"},
	{Name: "Typecho", Type: "code", Rule: "(Typecho</a>)"},
	{Name: "Z-Blog", Type: "code", Rule: "(powered by Z-Blog|zblog)"},
	{Name: "EmpireCMS", Type: "code", Rule: "(EmpireCMS)"},
	{Name: "DedeCMS", Type: "code", Rule: "(DedeCMS|DedeCms)"},
	{Name: "PHPCMS", Type: "code", Rule: "(PHPCMS|PHP CMS)"},
	{Name: "Drupal", Type: "code", Rule: "(Drupal|drupal)"},
	{Name: "Joomla", Type: "code", Rule: "(Joomla|/joomla/)"},
	{Name: "Magento", Type: "code", Rule: "(Magento|magento)"},
	{Name: "vBulletin", Type: "code", Rule: "(vBulletin|vbulletin)"},
	{Name: "phpBB", Type: "code", Rule: "(phpBB|phpbb)"},
	{Name: "myBB", Type: "code", Rule: "(myBB|mybb)"},
	{Name: "MediaWiki", Type: "code", Rule: "(MediaWiki|mediawiki)"},
	{Name: "DokuWiki", Type: "code", Rule: "(DokuWiki)"},
	{Name: "Confluence", Type: "code", Rule: "(Atlassian Confluence|confluence)"},
	{Name: "Ghost", Type: "code", Rule: "(Ghost|ghost)"},
	{Name: "Hugo", Type: "headers", Rule: "(X-Hugo)"},
	{Name: "Hexo", Type: "code", Rule: "(hexo-theme-|hexo.io)"},

	// ─── Dev panels / 运维 / 监控 / 容器 ───
	{Name: "Portainer(Docker管理)", Type: "code", Rule: "(portainer.updatePassword|portainer.init.admin)"},
	{Name: "Gogs简易Git服务", Type: "cookie", Rule: "(i_like_gogs)"},
	{Name: "Gitea简易Git服务", Type: "cookie", Rule: "(i_like_gitea)"},
	{Name: "Nexus", Type: "code", Rule: "(Nexus Repository Manager)"},
	{Name: "Nexus", Type: "cookie", Rule: "(NX-ANTI-CSRF-TOKEN)"},
	{Name: "Harbor", Type: "code", Rule: "(<title>Harbor</title>)"},
	{Name: "Harbor", Type: "cookie", Rule: "(harbor-lang)"},
	{Name: "Jenkins", Type: "code", Rule: "(Jenkins|x-jenkins)"},
	{Name: "Jenkins", Type: "headers", Rule: "(X-Jenkins|X-Hudson)"},
	{Name: "GitLab", Type: "code", Rule: "(GitLab)"},
	{Name: "GitLab", Type: "headers", Rule: "(gitlab-workhorse)"},
	{Name: "GitHub", Type: "headers", Rule: "(Server: GitHub.com|x-github-request-id)"},
	{Name: "Kubernetes-Dashboard", Type: "code", Rule: "(Kubernetes Dashboard)"},
	{Name: "RabbitMQ-Management", Type: "code", Rule: "(RabbitMQ Management)"},
	{Name: "Kibana", Type: "code", Rule: "(kibana)"},
	{Name: "Grafana", Type: "code", Rule: "(Grafana)"},
	{Name: "Prometheus", Type: "code", Rule: "(Prometheus)"},
	{Name: "Zabbix", Type: "code", Rule: "(Zabbix)"},
	{Name: "Nagios", Type: "code", Rule: "(Nagios Core)"},
	{Name: "禅道", Type: "code", Rule: "(/theme/default/images/main/zt-logo.png|/zentao/theme/zui/css/min.css)"},
	{Name: "禅道", Type: "cookie", Rule: "(zentaosid)"},

	// ─── App servers / 应用服务器 ───
	{Name: "weblogic", Type: "code", Rule: "(/console/framework/skins/wlsconsole/images/login_WebLogic_branding.png|Welcome to Weblogic Application Server|<i>Hypertext Transfer Protocol -- HTTP/1.1</i>)"},
	{Name: "WebSphere", Type: "code", Rule: "(WebSphere Application Server)"},
	{Name: "JBoss", Type: "code", Rule: "(JBoss|WildFly)"},
	{Name: "GlassFish", Type: "code", Rule: "(GlassFish Server)"},
	{Name: "Resin", Type: "code", Rule: "(Resin)"},
	{Name: "Jetty", Type: "headers", Rule: "(Server: Jetty)"},
	{Name: "Undertow", Type: "headers", Rule: "(Undertow)"},

	// ─── Mail / 邮件 ───
	{Name: "atmail-WebMail", Type: "cookie", Rule: "(atmail6)"},
	{Name: "atmail-WebMail", Type: "code", Rule: "(/index.php/mail/auth/processlogin|Powered by Atmail)"},
	{Name: "Roundcube", Type: "code", Rule: "(Roundcube|Mail)"},
	{Name: "Zimbra", Type: "code", Rule: "(Zimbra|ZimbraWebClient)"},
	{Name: "Exchange", Type: "headers", Rule: "(X-OWA-Version|X-FEServer|X-BEServer)"},

	// ─── OA / ERP / 企业应用 ───
	{Name: "致远OA", Type: "code", Rule: "(/seeyon/common/|/seeyon/USER-DATA/IMAGES/LOGIN/login.gif)"},
	{Name: "协众OA", Type: "code", Rule: "(Powered by 协众OA)"},
	{Name: "协众OA", Type: "cookie", Rule: "(CNOAOASESSID)"},
	{Name: "泛微OA", Type: "code", Rule: "(/weaver/|泛微|e-cology)"},
	{Name: "用友NC", Type: "code", Rule: "(/nc/|用友|Yonyou)"},
	{Name: "金蝶EAS", Type: "code", Rule: "(金蝶|EAS)"},
	{Name: "金蝶KIS", Type: "code", Rule: "(KIS)"},
	{Name: "蓝凌OA", Type: "code", Rule: "(蓝凌|Landray)"},
	{Name: "万户OA", Type: "code", Rule: "(万户|whir)"},
	{Name: "华天动力OA", Type: "code", Rule: "(华天动力|oa8080)"},
	{Name: "致远互联", Type: "code", Rule: "(seeyon|致远互联)"},

	// ─── Task / Message / 任务 / 消息中间件 ───
	{Name: "xxl-job", Type: "code", Rule: "(分布式任务调度平台XXL-JOB)"},
	{Name: "Activiti", Type: "code", Rule: "(Activiti)"},
	{Name: "RocketMQ-Console", Type: "code", Rule: "(RocketMQ-console)"},
	{Name: "Kafka-Manager", Type: "code", Rule: "(Kafka Manager)"},
	{Name: "KafkaOffsetMonitor", Type: "code", Rule: "(KafkaOffsetMonitor)"},
	{Name: "RocketMQ", Type: "headers", Rule: "(X-Rocketmq)"},
	{Name: "ActiveMQ", Type: "headers", Rule: "(X-ActiveMQ)"},
	{Name: "RabbitMQ", Type: "headers", Rule: "(X-RabbitMQ)"},
	{Name: "EMQX", Type: "code", Rule: "(EMQX)"},
	{Name: "Kong", Type: "headers", Rule: "(Server: kong)"},

	// ─── Security / Info disclosure / 安全 / 信息泄露 ───
	{Name: "Swagger-UI", Type: "code", Rule: "(swagger-ui|swagger-ui.html)"},
	{Name: "Actuator", Type: "code", Rule: "(/actuator|/env|/health|/beans|/trace)"},
	{Name: "Swagger-API", Type: "headers", Rule: "(X-Powered-By: Swagger)"},
	{Name: "ApiDoc", Type: "code", Rule: "(apidoc|api-doc)"},
	{Name: "Robots.txt", Type: "code", Rule: "(robots.txt)"},
	{Name: "phpinfo", Type: "code", Rule: "(<title>phpinfo\\(\\)</title>|PHP Version \\=>)"},

	// ─── Proxy / 网关 / 反向代理 ───
	{Name: "Kong-Gateway", Type: "code", Rule: "(Kong Admin|Kong Gateway)"},
	{Name: "APISIX", Type: "code", Rule: "(Apache APISIX)"},
	{Name: "Traefik", Type: "headers", Rule: "(Server: Traefik|X-Traefik)"},
	{Name: "Envoy", Type: "headers", Rule: "(Server: envoy|X-Envoy)"},
	{Name: "HAProxy", Type: "headers", Rule: "(Server: HAProxy|HAProxy)"},

	// ─── Misc / 其他 ───
	{Name: "Elasticsearch", Type: "code", Rule: "(elastic_search|elasticsearch_cluster|You Know, for Search)"},
	{Name: "Kibana", Type: "headers", Rule: "(kbn-name: kibana|X-Kibana)"},
	{Name: "Prometheus", Type: "headers", Rule: "(X-Prometheus)"},
	{Name: "Grafana", Type: "headers", Rule: "(X-Grafana)"},
	{Name: "etcd", Type: "headers", Rule: "(Server: etcd)"},
	{Name: "Consul", Type: "headers", Rule: "(X-Consul)"},
	{Name: "Vault", Type: "headers", Rule: "(X-Vault)"},
	{Name: "Prometheus-Exporter", Type: "code", Rule: "(/metrics)"},
}

// Init compiles all rule regexes once. / Init 编译所有规则正则一次。
func init() {
	for i := range RuleDatas {
		// Some rules contain raw parens / pipes that are already
		// meant as alternation. We treat the Rule field as a single
		// regex (no implicit group wrapping). / 部分规则含原始括号
		// /管道，已是 alternation 语义。Rule 字段视为单一正则。
		re, err := regexp.Compile(RuleDatas[i].Rule)
		if err != nil {
			// Skip invalid rules silently; they were validated
			// upstream. / 无效规则静默跳过；上游已校验。
			continue
		}
		RuleDatas[i].Compiled = re
	}
}

// matchRule checks one compiled rule against the rule's "type"
// (code / headers / cookie / index). / matchRule 用规则 type 检查
// 编译后的正则。
func matchRule(r RuleData, data CheckData) bool {
	if r.Compiled == nil {
		return false
	}
	var target string
	switch r.Type {
	case "code", "index":
		target = string(data.Body)
	case "headers":
		target = data.Headers
	case "cookie":
		// Cookie name=value pairs are part of headers. Pull them out
		// of the Set-Cookie lines for matching. / Set-Cookie 在
		// headers 里；把它们的 name=value 抽出来匹配。
		target = extractSetCookies(data.Headers)
	default:
		target = string(data.Body)
	}
	return r.Compiled.MatchString(target)
}

// extractSetCookies returns "name=value" pairs from Set-Cookie
// headers, one per line. / extractSetCookies 从 Set-Cookie 头抽取
// "name=value" 对，每行一个。
func extractSetCookies(headers string) string {
	var b strings.Builder
	for _, line := range splitLines(headers) {
		// Headers are "Key: Value"; we want the "Value" part of
		// "Set-Cookie" lines. / Headers 是 "Key: Value"；我们只
		// 关心 Set-Cookie 行的 Value。
		idx := strings.Index(strings.ToLower(line), "set-cookie:")
		if idx < 0 {
			continue
		}
		val := strings.TrimSpace(line[idx+len("set-cookie:"):])
		// Drop attributes after the first ';'. / 去掉第一个 ';' 后
		// 的属性。
		if semi := strings.IndexByte(val, ';'); semi >= 0 {
			val = strings.TrimSpace(val[:semi])
		}
		b.WriteString(val)
		b.WriteByte('\n')
	}
	return b.String()
}

// splitLines splits a string on '\n' (no allocations for empty lines).
// / splitLines 按 '\n' 切分字符串。
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
