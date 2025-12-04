import { execSync } from 'child_process';
import fs from 'fs';
import path from 'path';
import { Ed25519Keypair } from '@iota/iota-sdk/keypairs/ed25519';
import { requestIotaFromFaucetV0 } from '@iota/iota-sdk/faucet';

const NETWORK_URL = "http://127.0.0.1:9000";
const FAUCET_URL = "http://127.0.0.1:9123/gas";

const ENV_PATH = path.resolve(__dirname, "../.env");
const CONTRACT_PATH = path.resolve(__dirname, "../contracts");

async function main() {
    console.log("🚀 Iniciando deploy...");

    // 1. Criar Admin
    const keypair = new Ed25519Keypair();
    const address = keypair.toIotaAddress();
    const secret = keypair.getSecretKey();

    console.log(`🔑 Admin: ${address}`);

    // 2. Faucet
    try {
        console.log("🚰 Faucet...");
        await requestIotaFromFaucetV0({ host: FAUCET_URL, recipient: address });
        await new Promise(r => setTimeout(r, 2000));
    } catch (_) {
        console.log("⚠ Faucet falhou (tudo bem)");
    }

    // 3. Criar admin.key
    const KEY_PATH = path.resolve(CONTRACT_PATH, "admin.key");
    fs.writeFileSync(KEY_PATH, secret);
    console.log("💾 admin.key criado.");

    // 4. Publicar
    console.log("📦 Publicando Move Package...");

    try {
        const output = execSync(
            `iota client publish --sender ${address} --gas-budget 500000000 --json`,
            { cwd: CONTRACT_PATH, encoding: "utf-8" }
        );

        const result = JSON.parse(output);

        // --- Package ID ---
        const published = result.object_changes.find((o: any) => o.type === "published");
        if (!published) throw new Error("Package ID não encontrado");
        const packageId = published.package_id;

        // --- AdminCap ID ---
        const adminCapObj = result.object_changes.find(
            (o: any) => o.type === "created" && o.object_type.includes("::core::AdminCap")
        );
        if (!adminCapObj) throw new Error("AdminCap não encontrado");
        const adminCapId = adminCapObj.object_id;

        console.log("✅ Deploy concluído!");
        console.log("📦 Package:", packageId);
        console.log("🔑 AdminCap:", adminCapId);

        // 5. Criar .env
        const env = `
NETWORK_URL=${NETWORK_URL}
FAUCET_URL=${FAUCET_URL}
PACKAGE_ID=${packageId}
ADMIN_CAP_ID=${adminCapId}
ADMIN_SECRET=${secret}
ADDRESS=${address}
`;
        fs.writeFileSync(ENV_PATH, env.trim());
        console.log("💾 .env salvo!");

    } catch (err: any) {
        console.error("❌ Erro ao publicar:", err.message);
        if (err.stdout) console.log(err.stdout.toString());
        if (err.stderr) console.log(err.stderr.toString());
    }
}

main();
