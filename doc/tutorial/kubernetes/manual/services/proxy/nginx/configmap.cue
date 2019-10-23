package kube

configMap: nginx: "nginx.conf": """
		events {
		    worker_connections 768;
		}
		http {
		    sendfile on;
		    tcp_nopush on;
		    tcp_nodelay on;
		    # needs to be high for some download jobs.
		    keepalive_timeout 400;
		    # proxy_connect_timeout  300;
		    proxy_send_timeout       300;
		    proxy_read_timeout       300;
		    send_timeout             300;

		    types_hash_max_size 2048;

		    include /etc/nginx/mime.types;
		    default_type application/octet-stream;

		    access_log /dev/stdout;
		    error_log  /dev/stdout;

		    # Disable POST body size constraints. We often deal with large
		    # files. Especially docker containers may be large.
		    client_max_body_size 0;

		    upstream goget {
		        server localhost:7070;
		    }

		    # Redirect incoming Google Cloud Storage notifications:
		   server {
		        listen 443 ssl;
		        server_name notify.example.com notify2.example.com;

		        ssl_certificate /etc/ssl/server.crt;
		        ssl_certificate_key /etc/ssl/server.key;

		        # Security enhancements to deal with poodles and the like.
		        # See https://raymii.org/s/tutorials/Strong_SSL_Security_On_nginx.html
		        # ssl_ciphers 'AES256+EECDH:AES256+EDH';
		        ssl_ciphers \"ECDHE-RSA-AES256-GCM-SHA384:ECDHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-SHA384:ECDHE-RSA-AES128-SHA256:ECDHE-RSA-AES256-SHA:ECDHE-RSA-AES128-SHA:DHE-RSA-AES256-SHA256:DHE-RSA-AES128-SHA256:DHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA:ECDHE-RSA-DES-CBC3-SHA:EDH-RSA-DES-CBC3-SHA:AES256-GCM-SHA384:AES128-GCM-SHA256:AES256-SHA256:AES128-SHA256:AES256-SHA:AES128-SHA:DES-CBC3-SHA:HIGH:!aNULL:!eNULL:!EXPORT:!DES:!MD5:!PSK:!RC4\";

		        # We don't like poodles.
		        ssl_protocols TLSv1 TLSv1.1 TLSv1.2;
		        ssl_session_cache shared:SSL:10m;

		        # Enable Forward secrecy.
		        ssl_dhparam /etc/ssl/dhparam.pem;
		        ssl_prefer_server_ciphers on;

		        # Enable HTST.
		        add_header Strict-Transport-Security max-age=1209600;

		        # required to avoid HTTP 411: see Issue #1486 (https://github.com/dotcloud/docker/issues/1486)
		        chunked_transfer_encoding on;

		        location / {
		            proxy_pass http://tasks:7080;
		            proxy_connect_timeout 1;
		        }
		    }

		    server {
		        listen 80;
		        listen 443 ssl;
		        server_name x.example.com example.io;

		        location ~ \"(/[^/]+)(/.*)?\" {
		            set $myhost $host;
		            if ($arg_go-get = \"1\") {
		                set $myhost \"goget\";
		            }
		            proxy_pass http://$myhost$1;
		            proxy_set_header Host $host;
		            proxy_set_header X-Real-IP $remote_addr;
		            proxy_set_header X-Scheme $scheme;
		            proxy_connect_timeout 1;
		        }

		        location / {
		            set $myhost $host;
		            if ($arg_go-get = \"1\") {
		                set $myhost \"goget\";
		            }
		            proxy_pass http://$myhost;
		            proxy_set_header Host $host;
		            proxy_set_header X-Real-IP $remote_addr;
		            proxy_set_header X-Scheme $scheme;
		            proxy_connect_timeout 1;
		        }
		    }

		    server {
		        listen 80;
		        server_name www.example.com w.example.com;

		        resolver 8.8.8.8;

		        location / {
		            proxy_set_header X-Forwarded-Host $host;
		            proxy_set_header X-Forwarded-Server $host;
		            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
		            proxy_set_header X-Real-IP $remote_addr;

		            proxy_pass http://$host.default.example.appspot.com/$request_uri;
		            proxy_redirect http://$host.default.example.appspot.com/ /;
		        }
		    }

		    # Kubernetes URI space. Maps URIs paths to specific servers using the
		    # proxy.
		    server {
		        listen 80;
		        listen 443 ssl;
		        server_name proxy.example.com;

		        ssl_certificate /etc/ssl/server.crt;
		        ssl_certificate_key /etc/ssl/server.key;

		        # Security enhancements to deal with poodles and the like.
		        # See https://raymii.org/s/tutorials/Strong_SSL_Security_On_nginx.html
		        # ssl_ciphers 'AES256+EECDH:AES256+EDH';
		        ssl_ciphers \"ECDHE-RSA-AES256-GCM-SHA384:ECDHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-SHA384:ECDHE-RSA-AES128-SHA256:ECDHE-RSA-AES256-SHA:ECDHE-RSA-AES128-SHA:DHE-RSA-AES256-SHA256:DHE-RSA-AES128-SHA256:DHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA:ECDHE-RSA-DES-CBC3-SHA:EDH-RSA-DES-CBC3-SHA:AES256-GCM-SHA384:AES128-GCM-SHA256:AES256-SHA256:AES128-SHA256:AES256-SHA:AES128-SHA:DES-CBC3-SHA:HIGH:!aNULL:!eNULL:!EXPORT:!DES:!MD5:!PSK:!RC4\";

		        # We don't like poodles.
		        ssl_protocols TLSv1 TLSv1.1 TLSv1.2;
		        ssl_session_cache shared:SSL:10m;

		        # Enable Forward secrecy.
		        ssl_dhparam /etc/ssl/dhparam.pem;
		        ssl_prefer_server_ciphers on;

		        # Enable HTST.
		        add_header Strict-Transport-Security max-age=1209600;

		        if ($ssl_protocol = \"\") {
		            rewrite ^   https://$host$request_uri? permanent;
		        }

		        # required to avoid HTTP 411: see Issue #1486 (https://github.com/dotcloud/docker/issues/1486)
		        chunked_transfer_encoding on;

		        location / {
		            proxy_pass http://kubeproxy:4180;
		            proxy_set_header Host $host;
		            proxy_set_header X-Real-IP $remote_addr;
		            proxy_set_header X-Scheme $scheme;
		            proxy_connect_timeout 1;
		        }
		    }

		    server {
		        # We could add the following line and the connection would still be SSL,
		        # but it doesn't appear to be necessary. Seems saver this way.
		        listen 80;
		        listen 443 default ssl;
		        server_name ~^(?<sub>.*)\\.example\\.com$;

		        ssl_certificate /etc/ssl/server.crt;
		        ssl_certificate_key /etc/ssl/server.key;

		        # Security enhancements to deal with poodles and the like.
		        # See https://raymii.org/s/tutorials/Strong_SSL_Security_On_nginx.html
		        # ssl_ciphers 'AES256+EECDH:AES256+EDH';
		        ssl_ciphers \"ECDHE-RSA-AES256-GCM-SHA384:ECDHE-RSA-AES128-GCM-SHA256:DHE-RSA-AES256-GCM-SHA384:DHE-RSA-AES128-GCM-SHA256:ECDHE-RSA-AES256-SHA384:ECDHE-RSA-AES128-SHA256:ECDHE-RSA-AES256-SHA:ECDHE-RSA-AES128-SHA:DHE-RSA-AES256-SHA256:DHE-RSA-AES128-SHA256:DHE-RSA-AES256-SHA:DHE-RSA-AES128-SHA:ECDHE-RSA-DES-CBC3-SHA:EDH-RSA-DES-CBC3-SHA:AES256-GCM-SHA384:AES128-GCM-SHA256:AES256-SHA256:AES128-SHA256:AES256-SHA:AES128-SHA:DES-CBC3-SHA:HIGH:!aNULL:!eNULL:!EXPORT:!DES:!MD5:!PSK:!RC4\";

		        # We don't like poodles.
		        ssl_protocols TLSv1 TLSv1.1 TLSv1.2;
		        ssl_session_cache shared:SSL:10m;

		        # Enable Forward secrecy.
		        ssl_dhparam /etc/ssl/dhparam.pem;
		        ssl_prefer_server_ciphers on;

		        # Enable HTST.
		        add_header Strict-Transport-Security max-age=1209600;

		        if ($ssl_protocol = \"\") {
		            rewrite ^   https://$host$request_uri? permanent;
		        }

		        # required to avoid HTTP 411: see Issue #1486 (https://github.com/dotcloud/docker/issues/1486)
		        chunked_transfer_encoding on;

		        location / {
		            proxy_pass http://authproxy:4180;
		            proxy_set_header Host $host;
		            proxy_set_header X-Real-IP $remote_addr;
		            proxy_set_header X-Scheme $scheme;
		            proxy_connect_timeout 1;
		        }
		    }
		}
		"""
