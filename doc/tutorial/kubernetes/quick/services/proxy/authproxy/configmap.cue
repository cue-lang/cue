package kube

configMap authproxy: {
	// To update run:
	// kubectl apply -f configmap.yaml
	// kubectl scale --replicas=0 deployment/proxy
	// kubectl scale --replicas=1 deployment/proxy
	apiVersion: "v1"
	kind:       "ConfigMap"
	data "authproxy.cfg": """
		# Google Auth Proxy Config File
		## https://github.com/bitly/google_auth_proxy

		## <addr>:<port> to listen on for HTTP clients
		http_address = \"0.0.0.0:4180\"

		## the OAuth Redirect URL.
		redirect_url = \"https://auth.example.com/oauth2/callback\"

		## the http url(s) of the upstream endpoint. If multiple, routing is based on path
		upstreams = [
		    # frontend
		    \"http://frontend-waiter:7080/dpr/\",
		    \"http://frontend-maitred:7080/ui/\",
		    \"http://frontend-maitred:7080/ui\",
		    \"http://frontend-maitred:7080/report/\",
		    \"http://frontend-maitred:7080/report\",
		    \"http://frontend-maitred:7080/static/\",
		    # kitchen
		    \"http://kitchen-chef:8080/visit\",
		    # infrastructure
		    \"http://download:7080/file/\",
		    \"http://download:7080/archive\",
		    \"http://tasks:7080/tasks\",
		    \"http://tasks:7080/tasks/\",
		]

		## pass HTTP Basic Auth, X-Forwarded-User and X-Forwarded-Email information to upstream
		pass_basic_auth = true
		request_logging = true

		## Google Apps Domains to allow authentication for
		google_apps_domains = [
		    \"example.com\",
		]

		email_domains = [
		    \"example.com\",
		]

		## The Google OAuth Client ID, Secret
		client_id = \"---\"
		client_secret = \"---\"

		## Cookie Settings
		## Secret - the seed string for secure cookies
		## Domain - optional cookie domain to force cookies to (ie: .yourcompany.com)
		## Expire - expire timeframe for cookie
		cookie_secret = \"won't tell you\"
		cookie_domain = \".example.com\"
		cookie_https_only = true
		"""
}
