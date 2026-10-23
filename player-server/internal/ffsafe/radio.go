package ffsafe

// PipeInputArgs admits an already-fetched live audio body on standard input.
// It shares the audited container whitelist with file playback, while refusing
// every protocol except the caller-owned pipe. In particular, a playlist cannot
// fetch its own files or network references outside the outbound HTTP policy.
func PipeInputArgs() []string {
	return []string{"-protocol_whitelist", "pipe", "-format_whitelist", FormatWhitelist, "-i", "pipe:0"}
}
