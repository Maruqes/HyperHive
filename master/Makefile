.PHONY: run first clear-nots

run:
	npm run build:stream-analytics
	go build
	sudo ./512SvMan

first:
	npm run build:stream-analytics
	go build
	sudo ./512SvMan --exit-after-start

clear-nots:
	sqlite3 data.db "DELETE FROM nots;"
