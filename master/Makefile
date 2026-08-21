.PHONY: run first

run:
	npm run build:stream-analytics
	go build
	sudo ./512SvMan

first:
	npm run build:stream-analytics
	go build
	sudo ./512SvMan --exit-after-start
