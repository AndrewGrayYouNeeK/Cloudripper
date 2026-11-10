// Package runtime is the shared orchestration used by the CloudRipper CLI and
// the read-only desktop app. It builds providers, scans, resolves cost, loads
// the embedded fictional demo, and checks local credential presence.
//
// It does not apply, scale, schedule, or run chaos experiments. Callers that
// mutate cloud resources live in the CLI.
package runtime
