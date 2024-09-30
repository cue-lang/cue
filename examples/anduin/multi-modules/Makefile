nuke:
	sudo rm -rf ./cue.cache/mod && \
		rm -rf ./data/* && \
		mkdir -p ./cue.cache ./data

publish-module1:
	cd ./module1 && \
		../cue.sh mod publish v1.0.1

eval-module2:
	cd ./module2 && \
		../cue.sh mod tidy && \
		../cue.sh export ./main.cue

oras-pull:
	cd data && \
		oras pull localhost:5000/anduin.com/module1:v1.0.1
