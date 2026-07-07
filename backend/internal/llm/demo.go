package llm

import "strings"

func demoGenerate(req GenerateRequest) GenerateResponse {
	prompt := strings.ToLower(req.Prompt)
	switch {
	case strings.Contains(prompt, "法兰") || strings.Contains(prompt, "flange"):
		return GenerateResponse{
			Code:        flangeCode(),
			Explanation: "Demo mode generated Cascade Studio JS for a flange-style part with a center bore and bolt circle.",
			Warnings: []string{
				"llm.apiKey is not configured; this is a deterministic demo response.",
				"The browser runs this Cascade Studio JavaScript directly with cascade-core.",
			},
		}
	case strings.Contains(prompt, "手机") || strings.Contains(prompt, "phone") || strings.Contains(prompt, "支架") || strings.Contains(prompt, "stand"):
		return GenerateResponse{
			Code:        phoneStandCode(),
			Explanation: "Demo mode generated Cascade Studio JS for a compact angled phone stand concept.",
			Warnings:    []string{"llm.apiKey is not configured; this is a deterministic demo response."},
		}
	default:
		return GenerateResponse{
			Code:        roundedBoxCode(),
			Explanation: "Demo mode generated Cascade Studio JS for a simple box with a center through-hole.",
			Warnings:    []string{"llm.apiKey is not configured; this is a deterministic demo response."},
		}
	}
}

func roundedBoxCode() string {
	return `// aiopencad-demo-shape: rounded-box
// Cascade Studio JS, units: millimeters

var body = Box(60, 40, 20, true);
var centerHole = Rotate([1, 0, 0], 90, Cylinder(7, 80, true));

Difference(body, [centerHole]);`
}

func phoneStandCode() string {
	return `// aiopencad-demo-shape: phone-stand
// Cascade Studio JS, units: millimeters

var base = Translate([0, 0, 4], Box(85, 68, 8, true));
var back = Translate([0, 24, 42], Rotate([1, 0, 0], 18, Box(85, 8, 72, true)));
var lip = Translate([0, -28, 12], Box(85, 12, 12, true));
var cableCut = Translate([0, -28, 12], Rotate([1, 0, 0], 90, Cylinder(6, 24, true)));

var stand = Union([base, back, lip]);
Difference(stand, [cableCut]);`
}

func flangeCode() string {
	return `// aiopencad-demo-shape: flange
// Cascade Studio JS, units: millimeters

var disk = Cylinder(42, 10, true);
var hub = Cylinder(18, 22, true);
var blank = Union([disk, hub]);

var centerBore = Cylinder(9, 32, true);
var boltHoleA = Translate([26, 0, 0], Cylinder(3.2, 16, true));
var boltHoleB = Translate([-26, 0, 0], Cylinder(3.2, 16, true));
var boltHoleC = Translate([0, 26, 0], Cylinder(3.2, 16, true));
var boltHoleD = Translate([0, -26, 0], Cylinder(3.2, 16, true));

Difference(blank, [centerBore, boltHoleA, boltHoleB, boltHoleC, boltHoleD]);`
}
