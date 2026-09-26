package pod

import (
	"fmt"
	"strings"
)

const ExportContainer = "rustnet-export"
const ExportPath = "/evidence"

// ExportFiles defines the complete set that must be copied before deletion.
func ExportFiles(format string) ([]string, error) {
	switch format {
	case "":
		return nil, nil
	case "jsonl":
		return []string{"connections.jsonl"}, nil
	case "pcapng":
		return []string{"capture.pcapng"}, nil
	case "pcap":
		return []string{"capture.pcap", "capture.pcap.connections.jsonl"}, nil
	case "both":
		return []string{"connections.jsonl", "capture.pcapng"}, nil
	default:
		return nil, fmt.Errorf("invalid output format %q: choose jsonl, pcapng, pcap, or both", format)
	}
}

func exportArgs(format string, args []string) ([]string, error) {
	if _, err := ExportFiles(format); err != nil {
		return nil, err
	}
	result := append([]string(nil), args...)
	if format == "" {
		return result, nil
	}
	for _, arg := range args {
		flag, _, _ := strings.Cut(arg, "=")
		switch flag {
		case "--json-log", "--pcap-export", "--pcapng-export":
			return nil, fmt.Errorf("%s conflicts with --output-dir; select exports with --output-format", flag)
		}
	}
	if format == "jsonl" || format == "both" {
		result = append(result, "--json-log", ExportPath+"/connections.jsonl")
	}
	if format == "pcapng" || format == "both" {
		result = append(result, "--pcapng-export", ExportPath+"/capture.pcapng")
	}
	if format == "pcap" {
		result = append(result, "--pcap-export", ExportPath+"/capture.pcap")
	}
	return result, nil
}

// Keep stdin attached to the TUI even though the child runs asynchronously.
// The helper requests shutdown through a file, never a host PID. Only this
// parent signals its child. Publish completion after wait, when writers closed.
const captureScript = `umask 077
exec 3<&0
rustnet "$@" <&3 3<&- &
exec 3<&-
child=$!
trap 'kill -TERM "$child" 2>/dev/null || true' INT TERM HUP
stopping=0
while kill -0 "$child" 2>/dev/null; do
 if [ "$stopping" = 0 ] && [ -f /evidence/.stop ]; then
  kill -TERM "$child" 2>/dev/null || true
  stopping=1
 fi
 sleep 0.2
done
wait "$child"
result=$?
# RustNet can drop to nobody and transfer output ownership. The parent keeps
# its own credentials, so the helper can read mode-0600 exports without caps.
for file in /evidence/*; do
 [ -f "$file" ] || continue
 chown 0:0 "$file" || exit 1
done
printf '%s\n' "$result" > /evidence/.exit-code.tmp
mv /evidence/.exit-code.tmp /evidence/.exit-code
exit "$result"`

const helperScript = `trap 'exit 0' INT TERM; while :; do sleep 3600 & wait $!; done`
