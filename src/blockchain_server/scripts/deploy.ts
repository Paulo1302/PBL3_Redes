import { execSync } from 'child_process';
import fs from 'fs';
import path from 'path';
// CORREÇÃO: Usando a SDK oficial da IOTA
import { Ed25519Keypair } from '@iota/iota-sdk/keypairs/ed25519';
import { getFullnodeUrl, IotaClient } from '@iota/iota-sdk/client';
import { requestIotaFromFaucetV0 } from '@iota/iota-sdk/faucet';

// Configurações
const NETWORK_URL = 'http://127.0.0.1:9000';
const FAUCET_URL = 'http://127.0.0.1:9123/gas';
const ENV_PATH = path.resolve(__dirname, '../.env');
const CONTRACT_PATH = path.resolve(__dirname, '../contracts');

async function main() {
    console.log("🚀 Iniciando Deploy Automatizado (Via IOTA SDK Nativo)...");

    // 1. Criar Carteira Admin para o Servidor
    const keypair = new Ed25519Keypair();
    // MUDANÇA: toSuiAddress() -> toIotaAddress()
    const address = keypair.toIotaAddress(); 
    const secret = keypair.getSecretKey();
    
    console.log(`👤 Admin Temporário (Servidor): ${address}`);

    // 2. Garantir Saldo
    console.log("🚰 Enchendo o tanque (Faucet)...");
    try {
        // MUDANÇA: requestSuiFromFaucetV0 -> requestIotaFromFaucetV0
        await requestIotaFromFaucetV0({ host: FAUCET_URL, recipient: address });
        
        // Espera um pouco mais para garantir que a rede local processou o bloco
        await new Promise(resolve => setTimeout(resolve, 3000));
    } catch (e) {
        console.warn("⚠️ Faucet falhou (pode ser rate limit ou rede offline). Tentando seguir...");
    }

    // 3. Publicar Contrato (Usando CLI do Sistema 'iota')
    console.log("📦 Publicando Smart Contract...");
    try {
        // Tenta garantir que a CLI global também tenha saldo, caso precise
        try { execSync(`iota client faucet`, { stdio: 'ignore' }); } catch(e) {}
        
        const output = execSync(
            `iota client publish --gas-budget 100000000 --json`, 
            { cwd: CONTRACT_PATH, encoding: 'utf-8' }
        );
        
        const result = JSON.parse(output);

        // 4. Extrair IDs do resultado JSON
        let packageId = '';
        let adminCapId = '';

        // Procura o ID do pacote publicado
        const publishedObj = result.objectChanges.find((o: any) => o.type === 'published');
        if (publishedObj) packageId = publishedObj.packageId;

        // Procura o ID do AdminCap (objeto criado pelo init do contrato)
        const createdObj = result.objectChanges.find((o: any) => 
            o.type === 'created' && o.objectType.includes('::core::AdminCap')
        );
        if (createdObj) adminCapId = createdObj.objectId;

        if (!packageId || !adminCapId) {
            throw new Error("Não consegui encontrar o PackageID ou AdminCap no output.");
        }

        console.log(`✅ Deploy Sucesso!`);
        console.log(`   📝 Package ID: ${packageId}`);
        console.log(`   🔑 Admin Cap:  ${adminCapId}`);

        // TRANSFERÊNCIA DO ADMIN CAP
        // O deploy via CLI deixa o AdminCap na carteira padrão do sistema.
        // Precisamos movê-lo para a carteira 'address' que o servidor Node.js controla.
        console.log(`🚚 Transferindo AdminCap para a carteira do servidor...`);
        execSync(
            `iota client transfer --to ${address} --object-id ${adminCapId} --gas-budget 10000000`, 
            { encoding: 'utf-8' }
        );

        // 5. Salvar .env
        const envContent = `
NETWORK_URL=${NETWORK_URL}
FAUCET_URL=${FAUCET_URL}
PACKAGE_ID=${packageId}
ADMIN_CAP_ID=${adminCapId}
ADMIN_SECRET=${secret} 
ADDRESS=${address} 
`;
        fs.writeFileSync(ENV_PATH, envContent.trim());
        console.log("💾 Arquivo .env atualizado!");

    } catch (error: any) {
        console.error("❌ Erro no deploy:", error.message || error);
        if (error.stdout) console.log(error.stdout.toString());
    }
}

main();