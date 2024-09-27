nuke:
	sudo rm -rf ./cue.cache/mod

publish-module1:
	cd ./module1 && \
		../cue.sh mod publish v1.0.1

eval-module2:
	cd ./module2 && \
		../cue.sh mod tidy && \
		../cue.sh export ./main.cue


