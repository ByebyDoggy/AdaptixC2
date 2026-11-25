all: clean
	@ echo "    * Building listener_filehost plugin"
	@ mkdir dist
	@ cp config.yaml ax_config.axs ./dist/
	@ GOWORK=off GOEXPERIMENT=jsonv2,greenteagc go build -buildmode=plugin -ldflags="-s -w" -o ./dist/listener_filehost.so pl_main.go pl_transport.go cloudflare.go
	@ echo "      done..."

clean:
	@ rm -rf dist
