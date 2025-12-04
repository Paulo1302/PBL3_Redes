import { execSync } from 'child_process';
import fs from 'fs';
import path from 'path';
// MUDANÇA: Usando a SDK estável do Sui (compatível com IOTA)
import { Ed25519Keypair } from '@mysten/sui/keypairs/ed25519';
import { requestSuiFromFaucetV0 } from '@mysten/sui/faucet';

// Configurações
const NETWORK_URL = 'http://127.0.0.1:9000';
const FAUCET_URL = 'http://127.0.0.1:9123/gas';
const ENV_PATH = path.resolve(__dirname, '../.env');
const CONTRACT_PATH = path.resolve(__dirname, '../contracts');

async function main() {
    console.log("🚀 Iniciando Deploy Automatizado (Via Camada de Compatibilidade)...");

    // 1. Criar Carteira Admin
    const keypair = new Ed25519Keypair();
    const address = keypair.toSuiAddress(); // Na IOTA é o mesmo formato
    const secret = keypair.getSecretKey();
    
    console.log(`👤 Admin Temporário: ${address}`);

    // 2. Garantir Saldo
    console.log("🚰 Enchendo o tanque (Faucet)...");
    try {
        await requestSuiFromFaucetV0({ host: FAUCET_URL, recipient: address });
        await new Promise(resolve => setTimeout(resolve, 2000));
    } catch (e) {
        console.warn("⚠️ Faucet falhou (pode ser que já tenha saldo ou rede offline)");
    }

    // 3. Publicar Contrato (Usando CLI do Sistema 'iota')
    console.log("📦 Publicando Smart Contract...");
    try {
        try { execSync(`iota client faucet`, { stdio: 'ignore' }); } catch(e) {}
        
        const output = execSync(
            `iota client publish --gas-budget 100000000 --json`, 
            { cwd: CONTRACT_PATH, encoding: 'utf-8' }
        );
        
        const result = JSON.parse(output);

        // 4. Extrair IDs
        let packageId = '';
        let adminCapId = '';

        const publishedObj = result.objectChanges.find((o: any) => o.type === 'published');
        if (publishedObj) packageId = publishedObj.packageId;

        const createdObj = result.objectChanges.find((o: any) => 
            o.type === 'created' && o.objectType.includes('::core::AdminCap')
        );
        if (createdObj) adminCapId = createdObj.objectId;

        if (!packageId || !adminCapId) {
            throw new Error("Não consegui encontrar o PackageID ou AdminCap.");
        }

        console.log(`✅ Deploy Sucesso!`);
        console.log(`   📝 Package ID: ${packageId}`);
        console.log(`   🔑 Admin Cap:  ${adminCapId}`);

        // TRANSFERÊNCIA DO ADMIN CAP (Critical Fix)
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