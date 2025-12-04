import { execSync } from 'child_process';
import fs from 'fs';
import path from 'path';
import { Ed25519Keypair } from '@iota/iota-sdk/keypairs/ed25519';
import { getFullnodeUrl, IotaClient } from '@iota/iota-sdk/client';
import { requestIotaFromFaucetV0 } from '@iota/iota-sdk/faucet';

// Configurações
const NETWORK_URL = 'http://127.0.0.1:9000';
const FAUCET_URL = 'http://127.0.0.1:9123/gas';
const ENV_PATH = path.resolve(__dirname, '../.env');
const CONTRACT_PATH = path.resolve(__dirname, '../contracts'); // Onde está o Move.toml

async function main() {
    console.log("🚀 Iniciando Deploy Automatizado...");

    // 1. Criar ou Carregar uma Carteira de Admin para este computador
    // Vamos gerar uma nova e salvar, ou usar uma existente se quiser persistir
    const keypair = new Ed25519Keypair();
    const address = keypair.toIotaAddress();
    const secret = keypair.getSecretKey();
    
    console.log(`👤 Admin Temporário: ${address}`);

    // 2. Garantir Saldo (Para ser "Gratuito")
    console.log("🚰 Enchendo o tanque (Faucet)...");
    try {
        await requestIotaFromFaucetV0({ host: FAUCET_URL, recipient: address });
        // Espera 2 segundos pro saldo confirmar
        await new Promise(resolve => setTimeout(resolve, 2000));
    } catch (e) {
        console.warn("⚠️ Faucet falhou (pode ser que já tenha saldo ou rede offline)");
    }

    // 3. Publicar o Contrato usando a CLI do sistema
    // Precisamos apontar para a pasta contracts
    console.log("📦 Publicando Smart Contract...");
    
    // A CLI precisa de uma carteira ativa. Vamos usar a importação temporária ou assumir a default.
    // Para simplificar neste script, vamos assumir que o usuário já tem o `iota` configurado,
    // mas vamos forçar o uso da carteira que acabamos de criar/financiar se fosse produção.
    // TRUQUE: Como a CLI é chata de configurar via script, vamos usar o client do TS para publicar se possível,
    // mas o SDK TS de 'publish' é complexo. Vamos usar a CLI do sistema e pegar o output JSON.
    
    try {
        // Primeiro, garante que temos saldo na CLI ativa também (caso seja diferente)
        execSync(`iota client faucet`, { stdio: 'ignore' });
        
        // Roda o publish e pega o JSON
        const output = execSync(
            `iota client publish --gas-budget 100000000 --json`, 
            { cwd: CONTRACT_PATH, encoding: 'utf-8' }
        );
        
        const result = JSON.parse(output);

        // 4. Extrair os IDs
        let packageId = '';
        let adminCapId = '';

        // Achar PackageID
        const publishedObj = result.objectChanges.find((o: any) => o.type === 'published');
        if (publishedObj) packageId = publishedObj.packageId;

        // Achar AdminCap
        // Procura um objeto criado que tenha "AdminCap" no tipo
        const createdObj = result.objectChanges.find((o: any) => 
            o.type === 'created' && o.objectType.includes('::core::AdminCap')
        );
        if (createdObj) adminCapId = createdObj.objectId;

        if (!packageId || !adminCapId) {
            throw new Error("Não consegui encontrar o PackageID ou AdminCap no resultado.");
        }

        console.log(`✅ Deploy Sucesso!`);
        console.log(`   📝 Package ID: ${packageId}`);
        console.log(`   🔑 Admin Cap:  ${adminCapId}`);

        // 5. Salvar no .env automaticamente
        const envContent = `
NETWORK_URL=${NETWORK_URL}
FAUCET_URL=${FAUCET_URL}
PACKAGE_ID=${packageId}
ADMIN_CAP_ID=${adminCapId}
ADMIN_SECRET=${secret} 
ADDRESS=${address} 
`;
        fs.writeFileSync(ENV_PATH, envContent.trim());
        console.log("💾 Arquivo .env atualizado automaticamente!");

    } catch (error: any) {
        console.error("❌ Erro no deploy:", error.message || error);
        // Se a CLI falhar, mostre o output
        if (error.stdout) console.log(error.stdout.toString());
        if (error.stderr) console.log(error.stderr.toString());
    }
}

main();