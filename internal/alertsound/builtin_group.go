package alertsound

import (
	"sort"
	"strings"
)

var builtinGroupOrder = []Group{GroupWindows, GroupAlarm, GroupRing, GroupOther}

// The trailing space in "windows " keeps names like "Windowsfoo.wav" out of GroupWindows.
func classifyBuiltinGroup(name string) Group {
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(lower, "windows "):
		return GroupWindows
	case strings.HasPrefix(lower, "alarm"):
		return GroupAlarm
	case strings.HasPrefix(lower, "ring"):
		return GroupRing
	default:
		return GroupOther
	}
}

func sortedBuiltinOptions(names []string) []Option {
	byGroup := make(map[Group][]string, len(builtinGroupOrder))
	for _, name := range names {
		g := classifyBuiltinGroup(name)
		byGroup[g] = append(byGroup[g], name)
	}
	var opts []Option
	for _, g := range builtinGroupOrder {
		group := byGroup[g]
		if len(group) == 0 {
			continue
		}
		sortBuiltinGroup(g, group)
		for _, name := range group {
			opts = append(opts, Option{
				Sound: Sound{Kind: KindBuiltin, Name: name},
				Label: builtinLabel(name),
				Group: g,
			})
		}
	}
	return opts
}

func sortBuiltinGroup(g Group, names []string) {
	sort.Slice(names, func(i, j int) bool {
		if g == GroupWindows {
			iLogon := strings.EqualFold(names[i], DefaultSound.Name)
			jLogon := strings.EqualFold(names[j], DefaultSound.Name)
			if iLogon != jLogon {
				return iLogon
			}
		}
		return strings.ToLower(names[i]) < strings.ToLower(names[j])
	})
}
