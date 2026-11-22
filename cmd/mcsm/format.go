package main

import (
	"fmt"
	"time"

	"github.com/emiHuy/mc-server-manager/internal/apitypes"
)

func autoRestartSummary(a apitypes.AutoRestartResponse) string {
	switch {
	case !a.Enabled:
		return "disabled"
	case a.GaveUp:
		return fmt.Sprintf("gave up after %d restarts", a.Restarts)
	case a.NextRetryAt != nil:
		wait := max(time.Until(*a.NextRetryAt), 0).Round(time.Second)
		return fmt.Sprintf("retrying in %s (attempt %d of %d)", wait, a.Restarts, a.MaxRestarts)
	case a.Restarts > 0:
		return fmt.Sprintf("enabled (%d of %d restarts used)", a.Restarts, a.MaxRestarts)
	default:
		return "enabled"
	}
}

func seconds(s int64) string {
	return (time.Duration(s) * time.Second).String()
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}

	div := uint64(unit)
	exp := 0

	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
