export FUNCTION_NAME=guide-cooker

ifndef INSTANCE_NAME
INSTANCE_NAME := ${USER}
endif
export INSTANCE_NAME

ifndef IMAGE
IMAGE = docker.test.com/test-guide-docker-build
endif
export IMAGE

default:

# Don't remake the make file.
Makefile: ;

.PHONY: FORCE
FORCE:

# Note: Commented out because this is a test project, and all resource names are fictitious.
# # Make changes and verify all tests passed.
# full-build: FORCE
# 	$(MAKE) build
# 	$(MAKE) test
# 	$(MAKE) vet

# # Execute build/deploy targets.
# build-deploy: FORCE
# 	$(MAKE) build
# 	$(MAKE) package
# 	$(MAKE) deploy

%:: FORCE
ifndef SKIP_PULL
	docker pull ${IMAGE} 1>/dev/null
endif
	docker run --rm ${IMAGE} get-run-script > run.sh
	chmod +x run.sh
	./run.sh $@
