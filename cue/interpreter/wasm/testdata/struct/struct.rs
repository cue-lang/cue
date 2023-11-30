/*
   cargo build --release --target wasm32-unknown-unknown && cp target/wasm32-unknown-unknown/release/struct.wasm .
*/

extern crate alloc;
extern crate core;
extern crate wee_alloc;

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

use alloc::vec::Vec;
use std::mem::MaybeUninit;

#[global_allocator]
static ALLOC: wee_alloc::WeeAlloc = wee_alloc::WeeAlloc::INIT;

#[cfg_attr(all(target_arch = "wasm32"), export_name = "allocate")]
#[no_mangle]
pub extern "C" fn _allocate(size: u32) -> *mut u8 {
    allocate(size as usize)
}

fn allocate(size: usize) -> *mut u8 {
    let vec: Vec<MaybeUninit<u8>> = Vec::with_capacity(size);

    Box::into_raw(vec.into_boxed_slice()) as *mut u8
}

#[cfg_attr(all(target_arch = "wasm32"), export_name = "deallocate")]
#[no_mangle]
pub unsafe extern "C" fn _deallocate(ptr: u32, size: u32) {
    deallocate(ptr as *mut u8, size as usize);
}

unsafe fn deallocate(ptr: *mut u8, size: usize) {
    let _ = Vec::from_raw_parts(ptr, 0, size);
}
