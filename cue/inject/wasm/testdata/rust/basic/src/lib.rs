#[no_mangle]
pub extern "C" fn add(a: i64, b: i64) -> i64 {
    a + b
}

#[no_mangle]
pub extern "C" fn mul(a: f64, b: f64) -> f64 {
    a * b
}

#[no_mangle]
pub extern "C" fn not(x: bool) -> bool {
    !x
}
