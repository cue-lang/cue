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
pub extern "C" fn is_prime(n: u64) -> bool {
    for k in 2..n {
        if n % k == 0 {
            return false;
        }
    }
    true
}

#[no_mangle]
pub extern "C" fn fact(n: u64) -> u64 {
    if n == 1 {
        return 1;
    }
    n * fact(n - 1)
}

#[no_mangle]
pub extern "C" fn neg32(x: i32) -> i32 {
    -x
}
