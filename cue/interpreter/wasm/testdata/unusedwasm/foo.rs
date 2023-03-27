/*
   rustc -O --target wasm32-wasi --crate-type cdylib -C link-arg=--strip-debug -Cpanic=abort $%
*/

#![no_std]

use core::panic::PanicInfo;

#[panic_handler]
fn panic(_info: &PanicInfo) -> ! {
    loop {}
}

#[no_mangle]
pub extern "C" fn add(a: i64, b: i64) -> i64 {
    a + b
}
