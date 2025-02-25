package sysinfo

import (
	"encoding/json"
	"strconv"
	"strings"
)

// BatteryHealth: износ батареи и то, что сейчас её питает.
type BatteryHealth struct {
	Cycles    int
	Capacity  int    // процент от заводской ёмкости
	Condition string // Good, Fair, Service Recommended
	Charge    int
	Charging  bool
	Full      bool
	Charger   string
}

// DesignCycles: столько циклов Apple считает нормальным ресурсом для MacBook последних лет.
const DesignCycles = 1000

// ParseSystemProfilerPower разбирает system_profiler SPPowerDataType -json.
func ParseSystemProfilerPower(data []byte) (BatteryHealth, bool) {
	var doc struct {
		Items []map[string]json.RawMessage `json:"SPPowerDataType"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return BatteryHealth{}, false
	}
	var b BatteryHealth
	found := false
	for _, item := range doc.Items {
		if raw, ok := item["sppower_battery_health_info"]; ok {
			var h struct {
				Cycles    int    `json:"sppower_battery_cycle_count"`
				Condition string `json:"sppower_battery_health"`
				Max       string `json:"sppower_battery_health_maximum_capacity"`
			}
			if json.Unmarshal(raw, &h) == nil {
				found = true
				b.Cycles, b.Condition = h.Cycles, h.Condition
				b.Capacity, _ = strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(h.Max, "%")))
			}
		}
		if raw, ok := item["sppower_battery_charge_info"]; ok {
			var c struct {
				Charge   int    `json:"sppower_battery_state_of_charge"`
				Charging string `json:"sppower_battery_is_charging"`
				Full     string `json:"sppower_battery_fully_charged"`
			}
			if json.Unmarshal(raw, &c) == nil {
				b.Charge, b.Charging, b.Full = c.Charge, c.Charging == "TRUE", c.Full == "TRUE"
			}
		}
		if raw, ok := item["sppower_ac_charger_name"]; ok {
			_ = json.Unmarshal(raw, &b.Charger)
		}
	}
	return b, found
}

// ParseSysBattery: Linux, файлы из /sys/class/power_supply/BAT*; ключ - имя файла.
func ParseSysBattery(files map[string]string) (BatteryHealth, bool) {
	get := func(k string) int {
		n, _ := strconv.Atoi(strings.TrimSpace(files[k]))
		return n
	}
	if files["capacity"] == "" {
		return BatteryHealth{}, false
	}
	b := BatteryHealth{Cycles: get("cycle_count"), Charge: get("capacity")}
	full, design := get("energy_full"), get("energy_full_design")
	if full == 0 {
		full, design = get("charge_full"), get("charge_full_design")
	}
	if design > 0 {
		b.Capacity = full * 100 / design
	}
	status := strings.TrimSpace(files["status"])
	b.Charging, b.Full = status == "Charging", status == "Full"
	return b, true
}
