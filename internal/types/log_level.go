// log_level.go — Log level usage guidelines.
// log_level.go — 日志级别使用指南。
//
// This file documents the intended usage of each log level in the
// FG-QiMen codebase. All developers should follow these conventions
// to ensure consistent logging behavior.
//
// 本文件记录 FG-QiMen 代码库中每个日志级别的预期用法。所有开发者应
// 遵循这些约定以确保日志行为一致。
package types

// Log level usage guidelines / 日志级别使用指南:
//
// Info: Normal operational messages — scan lifecycle, statistics, progress.
//   Examples:
//     - "[*] alive: 10/100 hosts responded"
//     - "[*] scan completed in 5m30s"
//     - "[*] resume: loaded 42 seen hashes from bbolt"
//
// Warn: Recoverable anomalies — network timeouts, connection resets,
//   plugin errors, credential auth failures that are not bugs.
//   Examples:
//     - "[!] scan probe error: connection reset"
//     - "[!] plugin worker panic: recovered"
//     - "[!] cred auth error: 192.168.1.5:22 [ssh]: dial tcp: i/o timeout"
//
// Error: Failures requiring operator attention — config errors,
//   persistence failures, unrecoverable I/O errors.
//   Examples:
//     - "[-] output error: permission denied"
//     - "[-] store error: database corrupted"
//
// Debug: Detailed debugging information — only shown with -v flag.
//   Examples:
//     - "[.] trying credential admin:admin on 192.168.1.5:22"
//     - "[.] banner received: SSH-2.0-OpenSSH_8.9"
//
// Success: Positive scan results — open ports, identified services.
//   Examples:
//     - "[+] 192.168.1.5:22  [ssh]  SSH-2.0-OpenSSH_8.9"
//
// CredFound: Credential hits — successful authentication discoveries.
//   Examples:
//     - "[!] 192.168.1.5:3306  [mysql]  root / password123"
