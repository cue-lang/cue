package package_less_import

import "istio.io/api/other/global_scope"

// PackageLessImport references GlobalMessage unqualified. GlobalMessage is
// declared in a package-less file, so it lives in the global proto scope and
// is reachable without qualification, exactly as protoc resolves it.
#PackageLessImport: {
	global?: global_scope.#GlobalMessage @protobuf(1,GlobalMessage)
}
