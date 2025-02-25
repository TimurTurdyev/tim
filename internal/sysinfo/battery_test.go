package sysinfo

import "testing"

func TestParseSystemProfilerPower(t *testing.T) {
	data := []byte(`{"SPPowerDataType":[{"_name":"spbattery_information",
"sppower_battery_charge_info":{"sppower_battery_fully_charged":"TRUE","sppower_battery_is_charging":"FALSE","sppower_battery_state_of_charge":100},
"sppower_battery_health_info":{"sppower_battery_cycle_count":47,"sppower_battery_health":"Good","sppower_battery_health_maximum_capacity":"87 %"}},
{"_name":"sppower_ac_charger_information","sppower_ac_charger_name":"140W USB-C Power Adapter","sppower_ac_charger_watts":"140"}]}`)
	b, ok := ParseSystemProfilerPower(data)
	if !ok || b.Cycles != 47 || b.Capacity != 87 || b.Condition != "Good" || b.Charge != 100 || !b.Full || b.Charging {
		t.Fatalf("%+v", b)
	}
	if b.Charger != "140W USB-C Power Adapter" {
		t.Fatalf("зарядка: %q", b.Charger)
	}
	// У настольного мака батареи нет - это не ошибка, а "нечего показать".
	if _, ok := ParseSystemProfilerPower([]byte(`{"SPPowerDataType":[{"_name":"sppower_information"}]}`)); ok {
		t.Fatal("без батареи ok должно быть false")
	}
}

func TestParseSysBattery(t *testing.T) {
	b, ok := ParseSysBattery(map[string]string{"capacity": "64\n", "cycle_count": "312\n",
		"energy_full": "42000000", "energy_full_design": "50000000", "status": "Charging\n"})
	if !ok || b.Capacity != 84 || b.Cycles != 312 || !b.Charging || b.Charge != 64 {
		t.Fatalf("%+v", b)
	}
}
