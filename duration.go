package tenant

import (
	"encoding/json"
	"fmt"
	"time"
)

// Duration is a time.Duration that reads from configuration as Go writes one,
// "1m" or "30s", as well as a number of nanoseconds.
type Duration time.Duration

// UnmarshalJSON reads "1m" or a number of nanoseconds.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		v, err := time.ParseDuration(s)
		if err != nil {
			return fmt.Errorf("tenant: %w", err)
		}
		*d = Duration(v)
		return nil
	}
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("tenant: a duration is a string like \"1m\" or a number of nanoseconds")
	}
	*d = Duration(n)
	return nil
}
