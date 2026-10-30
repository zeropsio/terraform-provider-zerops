package validators

import (
	"fmt"
	"strings"

	"github.com/zeropsio/terraform-provider-zerops/internal/client"
)

// DualModeFamilies derives availability choices from qualified catalogue versions.
// A nil catalogue uses the 2026-10-29 production/dev catalogue snapshot only
// when the catalogue cannot be read (including before provider configuration).
func DualModeFamilies(settings *client.Settings) map[string]bool {
	if settings == nil {
		return map[string]bool{"postgresql": true, "mariadb": true, "valkey": true, "clickhouse": true, "elasticsearch": true, "kafka": true, "nats": true, "qdrant": true, "seaweedfs": true, "typesense": true}
	}
	modes := make(map[string]uint8)
	for _, typ := range settings.ServiceTypes {
		for _, version := range typ.Versions {
			name, _, ok := strings.Cut(version.Name, "@")
			if !ok || strings.Contains(name, "/") {
				continue
			}
			family, mode, ok := strings.Cut(name, ":")
			if !ok || family == "" {
				continue
			}
			family = strings.ToLower(family)
			switch strings.ToLower(mode) {
			case "single":
				modes[family] |= 1
			case "ha":
				modes[family] |= 2
			}
		}
	}
	families := make(map[string]bool)
	for family, modes := range modes {
		if modes == 3 {
			families[family] = true
		}
	}
	return families
}

// AmbiguousServiceFamily returns the lowercase catalogue key for dual-mode
// shorthand, without exposing a configured version or spelling.
func AmbiguousServiceFamily(name string, families map[string]bool) string {
	if strings.ContainsAny(name, "/:") {
		return ""
	}
	family, _, _ := strings.Cut(name, "@")
	family = strings.ToLower(family)
	if families[family] {
		return family
	}
	return ""
}

// AmbiguousServiceMode returns a diagnostic detail only for dual-mode shorthand.
func AmbiguousServiceMode(name string, families map[string]bool) string {
	if AmbiguousServiceFamily(name, families) == "" {
		return ""
	}
	family, version, hasVersion := strings.Cut(name, "@")
	suffix := ""
	if hasVersion {
		suffix = "@" + version
	}
	return fmt.Sprintf("%q does not choose an availability mode; Zerops resolves this shorthand to a highly available (HA) service. Use %q for a single-node (NON_HA) service or %q for a highly available (HA) service.", name, family+":single"+suffix, family+":ha"+suffix)
}
