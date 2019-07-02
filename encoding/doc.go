// Copyright 2019 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package encoding contains subpackages to convert CUE to and from byte-level
// and textual representations.
//
// For some packages, CUE can be mapped to both concrete values and higher-level
// definitions. For instance, a Go value can be mapped based on its concrete
// values or on its underlying type. Similarly, the protobuf package can extract
// CUE definitions from .proto definitions files, but also convert proto
// messages to concrete values.
//
// To clarify between these cases, we adopt the following naming convention:
//
//    Name        Direction   Level    Example
//    Decode      x -> CUE    Value    Convert an incoming proto message to CUE
//    Encode      CUE -> x    Value    Convert CUE to JSON
//    Extract     x -> CUE    Type     Extract CUE definition from .proto file
//    Generate    CUE -> x    Type     Generate OpenAPI definition from CUE
//
// To be more precise, Decoders and Encoders deal with concrete values only.
//
// Unmarshal and Marshal are used if the respective Decoder and Encoder decode
// and encode from and to a stream of bytes.
package encoding
