package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// StripComments accepts the JSON-with-comments configuration shipped upstream,
// without corrupting URLs or escaped quotes inside strings.
func StripComments(b []byte) []byte {
	out := append([]byte{}, b...)
	inString, escape := false, false
	for i := 0; i < len(b); i++ {
		if inString {
			if escape {
				escape = false
			} else if b[i] == '\\' {
				escape = true
			} else if b[i] == '"' {
				inString = false
			}
			continue
		}
		if b[i] == '"' {
			inString = true
			continue
		}
		if i+1 < len(b) && b[i] == '/' && b[i+1] == '/' {
			for i < len(b) && b[i] != '\n' {
				out[i] = ' '
				i++
			}
			i--
			continue
		}
		if i+1 < len(b) && b[i] == '/' && b[i+1] == '*' {
			out[i] = ' '
			out[i+1] = ' '
			i += 2
			for i < len(b) {
				if i+1 < len(b) && b[i] == '*' && b[i+1] == '/' {
					out[i] = ' '
					out[i+1] = ' '
					i++
					break
				}
				if b[i] != '\n' && b[i] != '\r' {
					out[i] = ' '
				}
				i++
			}
		}
	}
	return bytes.TrimPrefix(out, []byte{0xef, 0xbb, 0xbf})
}
func ReadJSON(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if e = json.Unmarshal(StripComments(b), v); e != nil {
		return fmt.Errorf("%s: %w", path, e)
	}
	return nil
}

type DefaultUser struct {
	GameMaster         bool
	Level              int
	Exp                int
	Points             int
	HonorPoints        int
	PrefixID           int
	Kills              int
	Deaths             int
	Battles            int
	Win                int
	PasswordBoxes      int
	MileagePoints      int
	DefaultItems       []int
	PseudoDefaultItems []int
	Loadouts           map[string]map[string]int
	BuyMenu            map[string]map[string]int
}
type Config struct {
	HostName               string
	Description            string
	WelcomeMessage         string
	Port                   string
	MaxPlayers             int
	MaxRegistrationsPerIP  int
	InventorySlotMax       int
	BanListMaxSize         int
	SSL                    bool
	Crypt                  bool
	CheckClientBuild       bool
	AllowedClientTimestamp int
	AllowedLauncherVersion int
	Metadata               map[string]bool
	DefaultUser            DefaultUser
	Room                   struct {
		HostConnectingMethod int
		ValidateSettings     bool
	}
	GameMatch struct {
		CalcResults struct {
			GameModeCoefficient map[string]struct {
				Exp    int
				Points int
			}
			BonusPercentage struct {
				Items map[string]struct {
					Exp    int
					Points int
				}
				Classes map[string]struct {
					Exp    int
					Points int
				}
			}
		}
	}
	FlockingFlyerType        int
	DedicatedServerWhitelist []string
}

func Load(path string) (Config, error) {
	c := Config{HostName: "CSO Server", Port: "30002", MaxPlayers: 100, MaxRegistrationsPerIP: 3, InventorySlotMax: 3000, BanListMaxSize: 300, AllowedLauncherVersion: 67}
	c.DedicatedServerWhitelist = []string{"127.0.0.1"}
	if e := ReadJSON(path, &c); e != nil {
		return c, e
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 0 || port > 65535 {
		return c, fmt.Errorf("invalid Port %q", c.Port)
	}
	if c.MaxPlayers < 1 || c.MaxPlayers > 10000 {
		return c, fmt.Errorf("MaxPlayers must be 1..10000")
	}
	if c.BanListMaxSize < 0 || c.BanListMaxSize > 65535 {
		return c, fmt.Errorf("BanListMaxSize must be 0..65535")
	}
	if c.InventorySlotMax < 1 || c.InventorySlotMax > 65535-len(c.DefaultUser.DefaultItems) {
		return c, fmt.Errorf("InventorySlotMax plus default item slots must fit uint16")
	}
	if len(c.DefaultUser.PseudoDefaultItems) >= c.InventorySlotMax {
		return c, fmt.Errorf("initial items plus the initial counter item exceed InventorySlotMax")
	}
	for _, id := range c.DefaultUser.PseudoDefaultItems {
		if id < 1 || id > 65535 {
			return c, fmt.Errorf("pseudo-default item ID must be 1..65535")
		}
	}
	if c.Room.HostConnectingMethod < 0 || c.Room.HostConnectingMethod > 2 {
		return c, fmt.Errorf("Room.HostConnectingMethod must be 0, 1 or 2")
	}
	for _, bonus := range c.GameMatch.CalcResults.BonusPercentage.Classes {
		if bonus.Exp < 0 || bonus.Exp > 65535 || bonus.Points < 0 || bonus.Points > 65535 {
			return c, fmt.Errorf("class bonus percentages must be 0..65535")
		}
	}
	if c.SSL {
		return c, fmt.Errorf("SSL=true is not yet supported; use the upstream default SSL=false")
	}
	return c, nil
}
