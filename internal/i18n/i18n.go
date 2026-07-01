// Package i18n provides a tiny message-catalog translator for Polish and English.
package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync"
)

// Lang is a supported UI language.
type Lang string

const (
	EN Lang = "en"
	PL Lang = "pl"
)

// ID identifies a translatable message.
type ID string

// Message IDs. Keep them grouped by area.
const (
	MsgVersion           ID = "version"
	MsgConfigLoaded      ID = "config_loaded"
	MsgConfigError       ID = "config_error"
	MsgConnCheckStart    ID = "conn_check_start"
	MsgConnCheckPass     ID = "conn_check_pass"
	MsgConnCheckFail     ID = "conn_check_fail"
	MsgConnCheckHostOK   ID = "conn_check_host_ok"
	MsgConnCheckHostFail ID = "conn_check_host_fail"
	MsgICMPMode          ID = "icmp_mode"
	MsgICMPUnavailable   ID = "icmp_unavailable"
	MsgNoConnectivity    ID = "no_connectivity"
	MsgDiagHeader        ID = "diag_header"
	MsgDaemonStart       ID = "daemon_start"
	MsgDaemonStop        ID = "daemon_stop"
	MsgWebListening      ID = "web_listening"
	MsgQuitRequested     ID = "quit_requested"
	MsgReportUnavailable ID = "report_unavailable"
)

var catalogs = map[Lang]map[ID]string{
	EN: {
		MsgVersion:           "proby %s (commit %s, built %s, %s)",
		MsgConfigLoaded:      "Loaded configuration from %s",
		MsgConfigError:       "Configuration error: %v",
		MsgConnCheckStart:    "Checking connectivity to %d host(s)...",
		MsgConnCheckPass:     "Connectivity OK (%d/%d hosts reachable)",
		MsgConnCheckFail:     "No connectivity: all %d check host(s) failed",
		MsgConnCheckHostOK:   "  OK    %s  (%d/%d replies, avg %s)",
		MsgConnCheckHostFail: "  FAIL  %s  (%s)",
		MsgICMPMode:          "ICMP: %s",
		MsgICMPUnavailable:   "ICMP unavailable: %v",
		MsgNoConnectivity:    "The probe could not reach any of the configured check hosts. This usually means the local network or its uplink is down. The diagnostic report below can be sent to your network operator.",
		MsgDiagHeader:        "proby network diagnostic report",
		MsgDaemonStart:       "Starting proby daemon (instance %q)",
		MsgDaemonStop:        "Shutting down...",
		MsgWebListening:      "Web UI and metrics listening on http://%s",
		MsgQuitRequested:     "Quit requested from local UI",
		MsgReportUnavailable: "(unavailable: %s)",
	},
	PL: {
		MsgVersion:           "proby %s (commit %s, zbudowano %s, %s)",
		MsgConfigLoaded:      "Wczytano konfigurację z %s",
		MsgConfigError:       "Błąd konfiguracji: %v",
		MsgConnCheckStart:    "Sprawdzanie łączności z %d hostem/hostami...",
		MsgConnCheckPass:     "Łączność OK (%d/%d hostów osiągalnych)",
		MsgConnCheckFail:     "Brak łączności: wszystkie %d hostów testowych nieosiągalne",
		MsgConnCheckHostOK:   "  OK    %s  (%d/%d odpowiedzi, śr. %s)",
		MsgConnCheckHostFail: "  BŁĄD  %s  (%s)",
		MsgICMPMode:          "ICMP: %s",
		MsgICMPUnavailable:   "ICMP niedostępne: %v",
		MsgNoConnectivity:    "Sonda nie mogła połączyć się z żadnym ze skonfigurowanych hostów testowych. Zwykle oznacza to awarię sieci lokalnej lub łącza. Poniższy raport diagnostyczny można przesłać do operatora sieci.",
		MsgDiagHeader:        "Raport diagnostyczny sieci proby",
		MsgDaemonStart:       "Uruchamianie demona proby (instancja %q)",
		MsgDaemonStop:        "Zamykanie...",
		MsgWebListening:      "Interfejs WWW i metryki nasłuchują na http://%s",
		MsgQuitRequested:     "Żądanie zamknięcia z lokalnego interfejsu",
		MsgReportUnavailable: "(niedostępne: %s)",
	},
}

// Translator renders messages in a fixed language.
type Translator struct {
	lang Lang
}

var (
	mu       sync.RWMutex
	fallback = EN
)

// New returns a Translator for the given language, defaulting to English.
func New(lang Lang) *Translator {
	if _, ok := catalogs[lang]; !ok {
		lang = fallback
	}
	return &Translator{lang: lang}
}

// Lang reports the translator's active language.
func (t *Translator) Lang() Lang { return t.lang }

// T renders message id with printf-style args, falling back to English then to the
// raw id if the message is missing.
func (t *Translator) T(id ID, args ...any) string {
	mu.RLock()
	defer mu.RUnlock()
	if cat, ok := catalogs[t.lang]; ok {
		if s, ok := cat[id]; ok {
			return fmt.Sprintf(s, args...)
		}
	}
	if s, ok := catalogs[fallback][id]; ok {
		return fmt.Sprintf(s, args...)
	}
	return string(id)
}

// Resolve turns a configured language string ("auto", "en", "pl") into a Lang,
// consulting the OS locale when "auto".
func Resolve(configured string) Lang {
	switch strings.ToLower(strings.TrimSpace(configured)) {
	case "en":
		return EN
	case "pl":
		return PL
	case "", "auto":
		return detectOSLang()
	default:
		return fallback
	}
}

// detectOSLang inspects common locale environment variables. On Windows these are
// usually unset, so English is the safe default.
func detectOSLang() Lang {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		v := strings.ToLower(os.Getenv(key))
		if strings.HasPrefix(v, "pl") {
			return PL
		}
		if strings.HasPrefix(v, "en") {
			return EN
		}
	}
	return fallback
}
