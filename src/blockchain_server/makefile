help:
	@echo "Comandos:"
	@echo "  install-dependencies - Instalar dependencias."
	@echo "  deploy - Publicar contrato e gerar .env"
	@echo "  start - Iniciar o servidor"

install-dependencies:
	@rm -rf node_modules package-lock.json
	@npm install

deploy:
	@npm run deploy

start:
	@npm run start

iota-init:
	@iota start --with-faucet --force-regenesis