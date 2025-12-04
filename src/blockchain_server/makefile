help:
	@echo "Comandos:"
	@echo "  install-dependencies - Instalar dependencias do TypeScript."
	@echo "  iota-init - Iniciar blockchain para testes (IOTA)."
	@echo "  run-tsc - Rodar código TypeScript"


install-dependencies:
	@npm install @iota/iota-sdk
	@npm install nats
	@npm install dotenv

run-tsc:
	@npx tsx scripts/deploy.ts
	@npx tsx index.ts

iota-init:
	@iota start --with-faucet --force-regenesis

test:
	@gnome-terminal -- fish -c "make iota-init; fish"
	@sleep 3
	@npm install @iota/iota-sdk
	@make run-tsc