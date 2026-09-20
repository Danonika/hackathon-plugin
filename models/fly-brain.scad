// Fly + brain models, corrected spatial logic (Y = body length, Z = up).
// Fly: horizontal body, head forward along Y, thin raised wings, six legs.
// Brain: dense, jittered bumps instead of a regular grid.

$fn = 50;

module leg(angle) {
    color("DimGray")
    rotate([0, angle, 0])
    translate([0, 0, -8])
    cylinder(h = 10, r = 0.6, center = true);
}

module fly() {
    union() {
        // Body: length along Y, horizontal
        color("MediumPurple")
        scale([1.2, 2, 1]) sphere(r = 10);

        // Head: forward along Y, not up
        color("DarkSlateGray")
        translate([0, 18, 2]) sphere(r = 6);

        // Eyes
        color("Red") translate([-4, 21, 5]) sphere(r = 3);
        color("Red") translate([4, 21, 5]) sphere(r = 3);

        // Wings: thinner, raised above the body, spread to the sides
        color("LightCyan", 0.6)
        translate([-8, -2, 9])
        rotate([10, -20, 35])
        scale([1.5, 4, 0.05]) sphere(r = 5);

        color("LightCyan", 0.6)
        translate([8, -2, 9])
        rotate([10, 20, -35])
        scale([1.5, 4, 0.05]) sphere(r = 5);

        // Six thin legs under the body
        translate([-4, 6, -6]) leg(20);
        translate([4, 6, -6]) leg(-20);
        translate([-4, 0, -7]) leg(15);
        translate([4, 0, -7]) leg(-15);
        translate([-4, -6, -6]) leg(10);
        translate([4, -6, -6]) leg(-10);
    }
}

$fn = 20;

module brain_bumps() {
    for (x = [-10 : 2 : 10]) {
        for (y = [-18 : 2 : 18]) {
            if (x*x + y*y < 250) {
                // Jitter on all three axes breaks the regular grid
                ox = sin(y * 0.7) * 1.5;
                oy = cos(x * 0.7) * 1.5;
                oz = sin(x * y * 0.05) * 1.0;
                z = sqrt(max(0, 225 - (x*x + y*y*0.5)));
                translate([x * 0.8 + ox, y + oy, z * 0.9 + oz])
                sphere(r = 1.2 + sin(x * 5 + y * 0.3) * 0.3);
            }
        }
    }
}

module brain_half() {
    union() {
        scale([0.7, 1.4, 1]) sphere(r = 15);
        brain_bumps();
    }
}

module full_brain() {
    color("Pink") {
        translate([-1.5, 0, 0]) brain_half();
        translate([1.5, 0, 0]) mirror([1, 0, 0]) brain_half();
    }
}

fly();
translate([40, 0, 0]) full_brain();
