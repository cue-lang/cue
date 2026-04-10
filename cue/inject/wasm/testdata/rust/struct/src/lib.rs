mod mem;

#[repr(C)]
pub struct Vector2 {
    x: f64,
    y: f64,
}

#[repr(C)]
pub struct Vector3 {
    x: f64,
    y: f64,
    z: f64,
}

#[no_mangle]
pub extern "C" fn magnitude2(v: &Vector2) -> f64 {
    (v.x.powi(2) + v.y.powi(2)).sqrt()
}

#[no_mangle]
pub extern "C" fn magnitude3(v: &Vector3) -> f64 {
    (v.x.powi(2) + v.y.powi(2) + v.z.powi(2)).sqrt()
}

#[no_mangle]
pub extern "C" fn normalize2(v: &Vector2) -> Vector2 {
    let l = magnitude2(v);
    Vector2 {
        x: v.x / l,
        y: v.y / l,
    }
}

#[no_mangle]
pub extern "C" fn double3(v: &Vector3) -> Vector3 {
    Vector3 {
        x: v.x * 2.0,
        y: v.y * 2.0,
        z: v.z * 2.0,
    }
}

#[repr(C)]
pub struct Cornucopia {
    b: bool,
    n0: i16,
    n1: u8,
    n2: i64,
}

#[no_mangle]
pub extern "C" fn cornucopia(x: &Cornucopia) -> i64 {
    if x.b {
        return 42;
    }
    return x.n0 as i64 + x.n1 as i64 + x.n2;
}

#[repr(C)]
pub struct Foo {
    b: bool,
    bar: Bar,
}

#[repr(C)]
pub struct Bar {
    b: bool,
    baz: Baz,
    n: u16,
}

#[repr(C)]
pub struct Baz {
    vec: Vector2,
}

#[no_mangle]
pub extern "C" fn magnitude_foo(x: &Foo) -> f64 {
    magnitude2(&x.bar.baz.vec)
}
