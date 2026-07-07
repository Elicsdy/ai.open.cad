/*
 Oriental Pearl Tower (Shanghai) - 1:1 dimensional exterior model
 Units: millimeters
 Default scale is full-size. Keep model_scale = 1 for a true 1:1 export.

 Sourced dimensions used in this model:
 - Total height: 468 m
 - Main columns: diameter 9 m
 - Inclined supports: diameter 7 m, angle 60 degrees to ground
 - Lower sphere: diameter 50 m, center elevation 93 m
 - Upper sphere: diameter 45 m, center elevation 272.5 m
 - Space capsule: diameter 16 m, center elevation 342 m
 - Small spheres: 5 units, diameter about 12 m
 - Podium diameter: 158.4 m
 - Podium core diameter: 60 m

 Inferred dimensions:
 - Exact vertical placement of the 5 small spheres
 - The footprint radius of the inclined supports
 - External antenna taper and collar proportions
 - Podium terracing and beam thicknesses

 The inferred values are chosen to preserve the tower's known silhouette
 while keeping the model structurally coherent and visually faithful.
*/

$fn = 144;

model_scale = 1;
with_color = true;

m = 1000;

// Confirmed or widely repeated published dimensions
tower_total_h = 468 * m;
main_body_h = 350 * m;

podium_d = 158.4 * m;
podium_core_d = 60 * m;
podium_h = 28 * m;

main_col_d = 9 * m;
main_col_clear_gap = 7 * m; // treated as clear spacing between adjacent columns
main_col_side = main_col_d + main_col_clear_gap;
main_col_r = main_col_side / sqrt(3);
main_col_h = 287 * m;

brace_d = 7 * m;
brace_angle = 60;
brace_top_z = 93 * m;
brace_foot_r = main_col_r + brace_top_z / tan(brace_angle);

lower_sphere_d = 50 * m;
lower_sphere_r = lower_sphere_d / 2;
lower_sphere_z = 93 * m;

upper_sphere_d = 45 * m;
upper_sphere_r = upper_sphere_d / 2;
upper_sphere_z = 272.5 * m;

capsule_d = 16 * m;
capsule_r = capsule_d / 2;
capsule_z = 342 * m;

small_sphere_d = 12 * m;
small_sphere_r = small_sphere_d / 2;
small_sphere_track_r = 18 * m;
small_sphere_zs = [126, 147, 168, 189, 210] * m;

// Visible exterior detailing derived from photographs and published silhouette
beam_levels = [for (i = [0 : 19]) (46 + i * 11) * m];
beam_d = 1.4 * m;

lower_ring_z = 90 * m;
upper_glass_ring_z = 259 * m;
upper_obs_ring_z = 263 * m;
restaurant_ring_z = 267 * m;

base_color = [0.80, 0.82, 0.84];
structure_color = [0.74, 0.76, 0.78];
pearl_color = [0.72, 0.18, 0.24];
accent_color = [0.90, 0.45, 0.48];
glass_color = [0.58, 0.78, 0.88, 0.40];

function point_on_circle(r, a, z = 0) = [r * cos(a), r * sin(a), z];
function column_angle(i) = 90 + i * 120;
function small_sphere_angle(i) = 18 + i * 72;
function sub3(a, b) = [a[0] - b[0], a[1] - b[1], a[2] - b[2]];

module tint(c) {
    if (with_color) {
        color(c) children();
    } else {
        children();
    }
}

module cylinder_between(p1, p2, d1, d2 = undef) {
    v = sub3(p2, p1);
    l = norm(v);
    axis = cross([0, 0, 1], v);
    ang = l < 0.001 ? 0 : acos(v[2] / l);
    end_d = is_undef(d2) ? d1 : d2;

    translate(p1)
        rotate(a = ang, v = norm(axis) < 0.001 ? [1, 0, 0] : axis)
            cylinder(h = l, d1 = d1, d2 = end_d, center = false);
}

module torus(major_r, minor_r) {
    rotate_extrude(convexity = 10)
        translate([major_r, 0, 0])
            circle(r = minor_r);
}

module podium() {
    tint(base_color)
        union() {
            cylinder(d = podium_d, h = 4 * m);
            translate([0, 0, 4 * m])
                cylinder(d1 = podium_d, d2 = 146 * m, h = 6 * m);
            translate([0, 0, 10 * m])
                cylinder(d1 = 146 * m, d2 = 124 * m, h = 7 * m);
            translate([0, 0, 17 * m])
                cylinder(d1 = 124 * m, d2 = 98 * m, h = 7 * m);
            translate([0, 0, 24 * m])
                cylinder(d1 = 98 * m, d2 = 84 * m, h = 4 * m);

            translate([0, 0, podium_h])
                cylinder(d = podium_core_d, h = 18 * m);

            for (i = [0 : 2]) {
                a = column_angle(i);
                hull() {
                    translate(point_on_circle(brace_foot_r, a, 0))
                        cylinder(d = 22 * m, h = 8 * m);
                    translate(point_on_circle(main_col_r, a, 0))
                        cylinder(d = 18 * m, h = podium_h);
                }
            }
        }
}

module main_columns() {
    tint(structure_color)
        for (i = [0 : 2]) {
            translate(point_on_circle(main_col_r, column_angle(i), podium_h))
                cylinder(d = main_col_d, h = main_col_h - podium_h);
        }
}

module inclined_supports() {
    tint(structure_color)
        for (i = [0 : 2]) {
            cylinder_between(
                point_on_circle(brace_foot_r, column_angle(i), 0),
                point_on_circle(main_col_r, column_angle(i), brace_top_z),
                brace_d
            );
        }
}

module center_spine() {
    tint(structure_color)
        union() {
            translate([0, 0, podium_h])
                cylinder(d = 8 * m, h = lower_sphere_z + lower_sphere_r - podium_h);
            translate([0, 0, lower_sphere_z + lower_sphere_r])
                cylinder(d = 6.5 * m, h = upper_sphere_z - upper_sphere_r - (lower_sphere_z + lower_sphere_r));
            translate([0, 0, upper_sphere_z - upper_sphere_r])
                cylinder(d = 7 * m, h = upper_sphere_d);
            translate([0, 0, upper_sphere_z + upper_sphere_r])
                cylinder(d = 5.5 * m, h = capsule_z - capsule_r - (upper_sphere_z + upper_sphere_r));
            translate([0, 0, capsule_z - capsule_r])
                cylinder(d = 5 * m, h = capsule_d);
        }
}

module ring_beams() {
    tint(structure_color)
        for (z = beam_levels) {
            for (i = [0 : 2]) {
                cylinder_between(
                    point_on_circle(main_col_r, column_angle(i), z),
                    point_on_circle(main_col_r, column_angle((i + 1) % 3), z),
                    beam_d
                );
            }
        }
}

module small_spheres() {
    tint(pearl_color)
        for (i = [0 : 4]) {
            center = point_on_circle(small_sphere_track_r, small_sphere_angle(i), small_sphere_zs[i]);
            translate(center)
                sphere(d = small_sphere_d);

            // Short bridge back to the structural spine
            cylinder_between([0, 0, small_sphere_zs[i]], center, 2.0 * m);
        }
}

module large_spheres() {
    tint(pearl_color)
        union() {
            translate([0, 0, lower_sphere_z])
                sphere(d = lower_sphere_d);
            translate([0, 0, upper_sphere_z])
                sphere(d = upper_sphere_d);
            translate([0, 0, capsule_z])
                sphere(d = capsule_d);
        }
}

module observation_rings() {
    tint(accent_color)
        union() {
            translate([0, 0, lower_ring_z])
                torus(lower_sphere_r - 1.1 * m, 1.0 * m);
            translate([0, 0, upper_glass_ring_z])
                torus(upper_sphere_r - 1.4 * m, 0.9 * m);
            translate([0, 0, upper_obs_ring_z])
                torus(upper_sphere_r - 0.8 * m, 0.9 * m);
            translate([0, 0, restaurant_ring_z])
                torus(upper_sphere_r - 0.5 * m, 0.8 * m);
        }

    tint(glass_color)
        translate([0, 0, upper_glass_ring_z])
            torus(upper_sphere_r - 1.0 * m, 0.5 * m);
}

module mast() {
    mast_base_z = capsule_z + capsule_r;

    tint(structure_color)
        union() {
            translate([0, 0, mast_base_z])
                cylinder(h = 8 * m, d1 = 8 * m, d2 = 6 * m);
            translate([0, 0, mast_base_z + 8 * m])
                cylinder(h = 42 * m, d = 6 * m);
            translate([0, 0, mast_base_z + 50 * m])
                cylinder(h = 30 * m, d1 = 6 * m, d2 = 4.5 * m);
            translate([0, 0, mast_base_z + 80 * m])
                cylinder(h = 24 * m, d1 = 4.5 * m, d2 = 3 * m);
            translate([0, 0, mast_base_z + 104 * m])
                cylinder(h = tower_total_h - (mast_base_z + 104 * m), d1 = 3 * m, d2 = 1.5 * m);

            for (z = [mast_base_z + 22 * m, mast_base_z + 46 * m, mast_base_z + 74 * m, mast_base_z + 98 * m]) {
                translate([0, 0, z])
                    torus(4.8 * m, 0.35 * m);
            }
        }
}

module ornamental_necks() {
    tint(accent_color)
        union() {
            translate([0, 0, lower_sphere_z + lower_sphere_r - 2.2 * m])
                torus(6.5 * m, 0.7 * m);
            translate([0, 0, upper_sphere_z - upper_sphere_r + 1.8 * m])
                torus(5.8 * m, 0.6 * m);
            translate([0, 0, upper_sphere_z + upper_sphere_r - 1.2 * m])
                torus(5.2 * m, 0.5 * m);
            translate([0, 0, capsule_z - capsule_r + 0.8 * m])
                torus(3.8 * m, 0.35 * m);
        }
}

module oriental_pearl_tower() {
    podium();
    inclined_supports();
    main_columns();
    center_spine();
    ring_beams();
    large_spheres();
    small_spheres();
    observation_rings();
    ornamental_necks();
    mast();
}

scale([model_scale, model_scale, model_scale])
    oriental_pearl_tower();
