// Package cloud holds service-Identify plugins for cloud-provider
// metadata endpoints (AWS IMDS, Azure Instance Metadata Service).
//
// / cloud 包是云供应商元数据端点（AWS IMDS、Azure Instance
// Metadata Service）的服务识别插件。
//
// All plugins in this package are HARD-rule compliant: they probe
// the metadata endpoint and report its presence + version. They
// do NOT exfiltrate credentials, tokens, or user data. / 本包
// 所有插件都符合 HARD 规则：探测元数据端点并报告其存在+版本。
// 绝不外泄凭据、token 或 user data。
package cloud
