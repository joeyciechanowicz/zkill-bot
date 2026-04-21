package zkill

import (
	"zkill-bot/internal/enrich/sde"
	"zkill-bot/internal/event"
)

// enrich annotates a zkill event with SDE-derived fields: ship/item/system
// names and the has_capital flag. Runs synchronously in the source because
// it's a pure in-memory map lookup with no external I/O.
func enrich(ev *event.Event) {
	var hasCapital bool

	if sysID, ok := ev.Fields["solar_system_id"].(int64); ok {
		ev.Fields["solar_system_name"] = sde.SystemName(sysID)
	}

	if v, ok := ev.Fields["victim"].(map[string]any); ok {
		if stID, ok := v["ship_type_id"].(int64); ok {
			if t, ok := sde.LookupType(stID); ok {
				v["ship_name"] = t.TypeName
				v["ship_group"] = t.GroupName
				v["ship_category"] = t.CatName
				v["ship_group_id"] = t.GroupID
				v["ship_category_id"] = t.CategoryID
				if sde.IsCapitalGroup(t.GroupID) {
					hasCapital = true
				}
			}
		}
	}

	if attackers, ok := ev.Fields["attackers"].([]any); ok {
		for _, a := range attackers {
			am, ok := a.(map[string]any)
			if !ok {
				continue
			}
			stID, _ := am["ship_type_id"].(int64)
			t, ok := sde.LookupType(stID)
			if !ok {
				continue
			}
			am["ship_name"] = t.TypeName
			am["ship_group"] = t.GroupName
			am["ship_category"] = t.CatName
			am["ship_group_id"] = t.GroupID
			am["meta_level"] = t.MetaLevel
			am["meta_group"] = t.MetaGroup
			if sde.IsCapitalGroup(t.GroupID) {
				hasCapital = true
			}
		}
	}

	if items, ok := ev.Fields["items"].([]any); ok {
		for _, it := range items {
			im, ok := it.(map[string]any)
			if !ok {
				continue
			}
			if tID, ok := im["item_type_id"].(int64); ok {
				if t, ok := sde.LookupType(tID); ok {
					im["name"] = t.TypeName
				}
			}
		}
	}

	ev.Fields["has_capital"] = hasCapital
}
