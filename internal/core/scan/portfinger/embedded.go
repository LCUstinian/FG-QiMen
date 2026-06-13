// Copyright (c) 2021 Insecure.Com LLC (Nmap project).
// SPDX-License-Identifier: Nmap Public Source License
//
// Embedded Nmap service probes database.
// Nmap service probes 数据库。
//
// The data is distributed under the Nmap Public Source License (see
// https://nmap.org/data/LICENSE). We //go:embed it and parse it at
// init; we DO NOT redistribute it under our own license.
//
// 数据按 Nmap Public Source License 分发（见
// https://nmap.org/data/LICENSE）。我们 //go:embed 引入并在 init 时
// 解析；不会以我们自己的许可证再分发。

package portfinger

import _ "embed"

//go:embed nmap-service-probes.txt
var embeddedProbes string
