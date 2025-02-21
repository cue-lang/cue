import (
	"encoding/json"
)

let defaultPolicy = json.Marshal({ deny: "all" })

#Spec: {
    policy?: string | *defaultPolicy
}
