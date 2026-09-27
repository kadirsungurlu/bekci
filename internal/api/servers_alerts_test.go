package api

import "testing"

func TestValidateAlertsMount(t *testing.T) {
	on := true
	ok := []alertInput{
		{Metric: "disk", Threshold: 85, Minutes: 1, Active: &on},
		{Metric: "disk", Mount: " /home ", Threshold: 90, Minutes: 1},
		{Metric: "disk", Mount: "/backup", Threshold: 95, Minutes: 5},
		{Metric: "cpu", Mount: "/yok-sayilir", Threshold: 90, Minutes: 10},
		{Metric: "disk", Mount: "D:", Threshold: 90, Minutes: 1},      // Windows sürücüsü
		{Metric: "disk", Mount: `C:\Veri`, Threshold: 90, Minutes: 1}, // klasöre bağlı birim
	}
	rules, err := validateAlerts(ok)
	if err != nil {
		t.Fatal(err)
	}
	if rules[1].Mount != "/home" || rules[3].Mount != "" {
		t.Fatalf("bölümler: %+v", rules)
	}
	for name, in := range map[string][]alertInput{
		"aynı bölüm iki kez": {{Metric: "disk", Mount: "/home", Threshold: 85, Minutes: 1}, {Metric: "disk", Mount: "/home", Threshold: 90, Minutes: 1}},
		"bölümsüz iki kez":   {{Metric: "disk", Threshold: 85, Minutes: 1}, {Metric: "disk", Threshold: 90, Minutes: 1}},
		"göreli yol":         {{Metric: "disk", Mount: "home", Threshold: 85, Minutes: 1}},
		"yarım sürücü":       {{Metric: "disk", Mount: "D:x", Threshold: 85, Minutes: 1}},
	} {
		if _, err := validateAlerts(in); err == nil {
			t.Errorf("%s: hata bekleniyordu", name)
		}
	}
}
