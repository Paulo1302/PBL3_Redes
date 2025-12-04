import { getFullnodeUrl, IotaClient } from '@iota/iota-sdk/client';
import { Ed25519Keypair } from '@iota/iota-sdk/keypairs/ed25519';
import { Transaction } from '@iota/iota-sdk/transactions';
import { requestIotaFromFaucetV0 } from '@iota/iota-sdk/faucet';
import * as nats from "nats";
import * as dotenv from 'dotenv';


dotenv.config();

// --- CONFIGURAÇÕES ---
const NETWORK_URL = process.env.NETWORK_URL || 'http://127.0.0.1:9000';
const FAUCET_URL = process.env.FAUCET_URL || 'http://127.0.0.1:9123/gas';
const PACKAGE_ID = process.env.PACKAGE_ID!;
const ADMIN_CAP_ID = process.env.ADMIN_CAP_ID!;
const ADMIN_SECRET = process.env.ADMIN_SECRET!;


function createWallet(){
    const keypair = new Ed25519Keypair();
    const secret = keypair.getSecretKey();
    const address = keypair.getPublicKey().toIotaAddress();

    console.log(`🆕 Carteira criada:`);
    console.log(`   🔑 PrivateKey: ${secret}`);
    console.log(`   📮 Address:    ${address}`);

    return { secret, address };
}

async function faucetWallet(addr: string) {
    console.log('🚰 Solicitando fundos ao Faucet...');
    try {
        await requestIotaFromFaucetV0({
            host: FAUCET_URL,
            recipient: addr,
        }); 
        return 1;
    } catch (e) {
        console.error("Erro no Faucet. Verifique se a rede local está rodando com --with-faucet");
        return 0;
    }
}

async function getBalance(addr: string, client: IotaClient) {
    let balance = 0, tries = 0;
    
    while (balance === 0 && tries < 15) {
        tries++;
        const { data } = await client.getCoins({ owner: addr });
        balance = data.reduce((sum, utxo) => sum + parseInt(utxo.balance), 0);

        if (balance === 0) {
            await new Promise(r => setTimeout(r, 1000));
        }
    }
    console.log(`💰 Saldo recebido: ${balance} NANOS`);
    return balance
}

function performTransaction(src: Ed25519Keypair, dest: string, amountToSend: number, client: IotaClient) {
    const tx = new Transaction();

    console.log("teste!!!");
    // Comando: SplitCoins (Tira do Gas) -> TransferObjects (Envia)
    const [coin] = tx.splitCoins(tx.gas, [amountToSend]);
    tx.transferObjects([coin], dest);
    console.log("teste2!!!");

    console.log('\n🚀 Enviando transação...');
    return client.signAndExecuteTransaction({
        signer: src,
        transaction: tx,
        options: {
            showEffects: true,
            showBalanceChanges: true,
        },
    });
}

async function handleCreateWallet(nc: nats.NatsConnection, jc: nats.Codec<unknown>){
    nc.subscribe("internalServer.wallet", {
        callback(err, msg) {
            const { secret, address } = createWallet()

            faucetWallet(address)
            
            const resposta = { ok: true, client: {secret, address} };
            msg.respond(jc.encode(resposta));
        },
    });
}

async function handleGetBalance(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient) {
    nc.subscribe("internalServer.balance", {
        async callback(err, msg) {
            const decMes = jc.decode(msg.data) as any;
            console.log("Recebi:", decMes);

            const iotaBalance = await getBalance(decMes.client.address, client)
            console.log("valor debug:", iotaBalance);
            const resposta = { ok: true, client: decMes.client, price: iotaBalance };
            msg.respond(jc.encode(resposta));
        },
    });
}

async function handleFaucetWallet(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient){
    nc.subscribe("internalServer.faucet", {
        async callback(err, msg) {
            const decMes = jc.decode(msg.data) as any;
            console.log("Recebi:", decMes);

            await faucetWallet(decMes.client.address);
            const iotaBalance = await getBalance(decMes.client.address, client)
            console.log("valor debug:", iotaBalance);
            const resposta = { ok: true, client: decMes.client, price: iotaBalance };
            msg.respond(jc.encode(resposta));
        },
    });
    
}


async function handleTransaction(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient){
    nc.subscribe("internalServer.transaction", {
        async callback(err, msg) {
            const decMes = jc.decode(msg.data) as any;
            console.log("Recebi Transação:", decMes);
            
            // Verifica saldo antes de tentar gastar
            // Usamos o endereço da chave pública derivada do segredo recebido
            const keypair = Ed25519Keypair.fromSecretKey(decMes.client.secret);
            const address = keypair.getPublicKey().toIotaAddress();

            await ensureFunds(address, client);

            const result = await performTransaction(keypair, decMes.aux_client.address, decMes.price, client)
            
            const resposta = { ok: result.effects?.status.status === 'success', client: decMes.client, };
            msg.respond(jc.encode(resposta));
        },
    });
}

// --- NOVOS HANDLERS PARA O GO CHAMAR ---

// 1. MINT (Abrir Pacote)
async function handleMintCard(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient, serverKeypair: Ed25519Keypair) {
    nc.subscribe("internalServer.mintCard", {
        async callback(err, msg) {
            const req = jc.decode(msg.data) as any;
            console.log(`📦 Mintando carta valor ${req.value} para ${req.address}`);

            const tx = new Transaction();
            tx.moveCall({
                target: `${PACKAGE_ID}::core::mint_card`,
                arguments: [
                    tx.object(ADMIN_CAP_ID),
                    tx.pure.u64(req.value),
                    tx.pure.address(req.address)
                ]
            });

            // O Servidor (Admin) assina a criação
            const result = await client.signAndExecuteTransaction({
                signer: serverKeypair,
                transaction: tx,
            });
            
            msg.respond(jc.encode({ ok: true, digest: result.digest }));
        }
    });
}

// 2. LOG MATCH (Salvar Resultado)
async function handleLogMatch(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient, serverKeypair: Ed25519Keypair) {
    nc.subscribe("internalServer.logMatch", {
        async callback(err, msg) {
            const req = jc.decode(msg.data) as any;
            
            const tx = new Transaction();
            tx.moveCall({
                target: `${PACKAGE_ID}::core::log_match`,
                arguments: [
                    tx.pure.address(req.winner),
                    tx.pure.address(req.loser),
                    tx.pure.u64(req.val_win),
                    tx.pure.u64(req.val_lose)
                ]
            });

            // Qualquer um pode pagar o gas desse log, vamos usar o server
            await client.signAndExecuteTransaction({ signer: serverKeypair, transaction: tx });
            msg.respond(jc.encode({ ok: true }));
        }
    });
}

// 3. TRANSFERÊNCIA DE CARTA (Troca)
// O Go manda o segredo do dono da carta para autorizar a transferência
async function handleTransferCard(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient) {
    nc.subscribe("internalServer.transferCard", {
        async callback(err, msg) {
            const req = jc.decode(msg.data) as any;
            
            // Recupera a carteira do dono da carta
            const signer = Ed25519Keypair.fromSecretKey(req.ownerSecret);
            
            const tx = new Transaction();
            tx.moveCall({
                target: `${PACKAGE_ID}::core::transfer_card`,
                arguments: [
                    tx.object(req.cardObjectId), // O ID do objeto na IOTA
                    tx.pure.address(req.recipient)
                ]
            });

            await client.signAndExecuteTransaction({ signer: signer, transaction: tx });
            msg.respond(jc.encode({ ok: true }));
        }
    });
}

// Garante que a carteira tenha dinheiro para operar
async function ensureFunds(address: string, client: IotaClient) {
    const { data } = await client.getCoins({ owner: address });
    const balance = data.reduce((sum, c) => sum + parseInt(c.balance), 0);
    
    // Se tiver menos de 0.5 IOTA (500.000.000 Nanos)
    if (balance < 500_000_000) {
        console.log(`📉 Saldo baixo (${balance}). Recarregando Faucet para ${address}...`);
        try {
            await requestIotaFromFaucetV0({
                host: process.env.FAUCET_URL!,
                recipient: address
            });
            // Espera o dinheiro cair
            await new Promise(r => setTimeout(r, 1000));
        } catch (e) {
            console.log("Erro no faucet (talvez rate limit), tentando seguir...");
        }
    }
}

async function main() {
    // 1. Validação de Segurança (Primeira coisa a fazer)
    if (!PACKAGE_ID || !ADMIN_SECRET) {
        console.error("❌ ERRO: Variáveis de ambiente não encontradas.");
        console.error("   Rode 'npm run deploy' primeiro!");
        process.exit(1);
    }

    // 2. Conexões de Infraestrutura
    const nc = await nats.connect({ servers: "localhost:4222" });
    const jc = nats.JSONCodec();
    
    const client = new IotaClient({ url: NETWORK_URL });
    console.log(`📡 Conectado à rede em: ${NETWORK_URL}`);

    // 3. Carregar Chave do Servidor (Antes de usar!)
    const serverKeypair = Ed25519Keypair.fromSecretKey(ADMIN_SECRET);
    console.log(`👑 Admin carregado: ${serverKeypair.getPublicKey().toIotaAddress()}`);

    console.log("🚀 Iniciando Workers...");

    // 4. Iniciar Handlers (Agora serverKeypair já existe)
    handleCreateWallet(nc, jc);
    handleFaucetWallet(nc, jc, client);
    handleGetBalance(nc, jc, client);
    
    // Pagamentos (Moeda)
    handleTransaction(nc, jc, client);

    // Smart Contract (NFTs e Log)
    handleMintCard(nc, jc, client, serverKeypair);
    handleLogMatch(nc, jc, client, serverKeypair);
    handleTransferCard(nc, jc, client);

    console.log("✅ Sistema pronto e escutando NATS.");
}
main().catch(console.error);