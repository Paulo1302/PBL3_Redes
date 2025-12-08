// Importa utilitários do IOTA SDK
import { getFullnodeUrl, IotaClient } from '@iota/iota-sdk/client';
import { Ed25519Keypair } from '@iota/iota-sdk/keypairs/ed25519';
import { Transaction } from '@iota/iota-sdk/transactions';
import { requestIotaFromFaucetV0 } from '@iota/iota-sdk/faucet';
// Importa NATS para mensagens e dotenv para carregar variáveis
import * as nats from "nats";
import * as dotenv from 'dotenv';

dotenv.config();

// --- CONFIGURAÇÕES ---
// URLs da rede, faucet e objetos principais, obtidos do .env
const NETWORK_URL = process.env.NETWORK_URL || 'http://127.0.0.1:9000';
const FAUCET_URL = process.env.FAUCET_URL || 'http://127.0.0.1:9123/gas';
const PACKAGE_ID = process.env.PACKAGE_ID!;
const ADMIN_CAP_ID = process.env.ADMIN_CAP_ID!;
const ADMIN_SECRET = process.env.ADMIN_SECRET!;

// --- ESTADO ---
// Fila de execução para garantir operações administrativas serializadas
let adminQueue = Promise.resolve();

// --- FUNÇÕES UTILITÁRIAS ---

// Cria uma carteira (par de chaves + endereço)
function createWallet(){
    const keypair = new Ed25519Keypair();
    const secret = keypair.getSecretKey();
    const address = keypair.toIotaAddress(); 
    return { secret, address };
}

// Retorna saldo total de uma carteira consultando UTXOs
async function getBalance(addr: string, client: IotaClient) {
    try {
        const { data } = await client.getCoins({ owner: addr });
        return data.reduce((sum, utxo) => sum + parseInt(utxo.balance), 0);
    } catch(e) { return 0; }
}

// Garante que a carteira possui saldo mínimo, pedindo ao Faucet se necessário
async function ensureFunds(address: string, client: IotaClient) {
    let balance = await getBalance(address, client);
    if (balance < 500_000_000) {
        console.log(`📉 Saldo baixo para operar (${balance}). Tentando Faucet...`);
        try {
            await requestIotaFromFaucetV0({ host: FAUCET_URL, recipient: address });
            await new Promise(r => setTimeout(r, 2000));
        } catch (e) {
            console.warn("⚠️ Falha ao pedir gás extra (pode ser ignorado se for Tesouraria).");
        }
    }
}

// Valida se um endereço é dono de um objeto on-chain
async function verifyOwnership(client: IotaClient, address: string, objectId: string): Promise<boolean> {
    try {
        const { data } = await client.getObject({ 
            id: objectId, 
            options: { showOwner: true } 
        });
        
        if (!data || !data.owner) return false;
        
        const ownerJson = data.owner as any;
        return ownerJson && ownerJson.AddressOwner === address;
    } catch (e) {
        console.error("Erro na validação de propriedade:", e);
        return false;
    }
}

// Garante que o administrador tem saldo altíssimo, usado como “tesouraria”
async function forceTreasuryRefill(address: string, client: IotaClient) {
    let balance = await getBalance(address, client);
    console.log(`🏦 Saldo Admin Atual: ${balance}`);
    while (balance < 50_000_000_000) {
        console.log(`📉 Saldo insuficiente. Marteland o Faucet...`);
        try { await requestIotaFromFaucetV0({ host: FAUCET_URL, recipient: address }); } catch (e) {}
        await new Promise(r => setTimeout(r, 2000));
        balance = await getBalance(address, client);
    }
    console.log("✅ Tesouraria Abastecida e Pronta.");
}

// Envia fundos para outro endereço
async function transferFunds(signer: Ed25519Keypair, dest: string, amount: number, client: IotaClient) {
    const tx = new Transaction();
    const [coin] = tx.splitCoins(tx.gas, [tx.pure.u64(amount)]);
    tx.transferObjects([coin], dest);
    return client.signAndExecuteTransaction({ signer, transaction: tx });
}

// --- EXECUÇÃO COM RETENTATIVA ---
// Executa uma transação Move com tentativas automáticas quando objeto está trancado
async function executeWithRetry(
    client: IotaClient, 
    signer: Ed25519Keypair, 
    buildTx: () => Transaction, 
    label: string
) {
    let attempts = 0;
    const MAX_ATTEMPTS = 15;

    while (attempts < MAX_ATTEMPTS) {
        try {
            if (attempts > 0) {
                const waitTime = attempts * 1000;
                console.log(`   ⚠️ Retentativa ${attempts}/${MAX_ATTEMPTS} para ${label} em ${waitTime}ms...`);
                try { await client.getObject({ id: ADMIN_CAP_ID }); } catch(e) {}
                await new Promise(r => setTimeout(r, waitTime)); 
            }

            const tx = buildTx();
            const res = await client.signAndExecuteTransaction({
                signer: signer,
                transaction: tx,
                options: { 
                    showEffects: true,
                    showObjectChanges: true 
                }
            });

            if (res.effects?.status.status === 'failure') {
                throw new Error(`Falha na execução: ${res.effects.status.error}`);
            }
            return res; 

        } catch (error: any) {
            const msg = (error?.message || JSON.stringify(error)).toLowerCase();
            const isRetriable = 
                msg.includes("not available") || 
                msg.includes("locked") || 
                msg.includes("version") || 
                msg.includes("notfound") || 
                msg.includes("could not find") ||
                msg.includes("transaction inputs");

            if (isRetriable) {
                attempts++;
            } else {
                console.error(`Erro fatal não retentável: ${msg}`);
                throw error; 
            }
        }
    }
    throw new Error(`Falha após ${MAX_ATTEMPTS} tentativas em ${label}`);
}

// --- HANDLERS ---
// Criação de nova carteira (chave + transferência inicial)
async function handleCreateWallet(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient, adminKey: Ed25519Keypair){
    nc.subscribe("internalServer.wallet", {
        callback(err, msg) {
            if (err) return;
            adminQueue = adminQueue.then(async () => {
                const { secret, address } = createWallet()
                console.log(`\n👤 Criando Usuário: ${address.substring(0,6)}...`);
                try {
                    await transferFunds(adminKey, address, 20_000_000_000, client);
                    
                    // Espera saldo confirmar na rede
                    process.stdout.write("   ⏳ Aguardando saldo confirmar");
                    let balance = 0, attempts = 0;
                    while (balance === 0 && attempts < 10) {
                        await new Promise(r => setTimeout(r, 1000));
                        balance = await getBalance(address, client);
                        process.stdout.write(".");
                        attempts++;
                    }
                    console.log(`\n   ✅ Saldo Confirmado: ${balance}`);

                    if (balance > 0) msg.respond(jc.encode({ ok: true, client: {secret, address} }));
                    else throw new Error("Saldo timeout");

                } catch (error) {
                    msg.respond(jc.encode({ ok: false }));
                }
            });
        }
    });
}

// Obtém o saldo de um usuário
async function handleGetBalance(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient) {
    nc.subscribe("internalServer.balance", {
        async callback(err, msg) {
            if (err) return;
            const d = jc.decode(msg.data) as any;
            const bal = await getBalance(d.client.address, client);
            msg.respond(jc.encode({ ok: true, client: d.client, price: bal }));
        },
    });
}

// Cobra usuário → transfere para outro
async function handleTransaction(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient){
    nc.subscribe("internalServer.transaction", {
        async callback(err, msg) {
            if (err) return;
            const d = jc.decode(msg.data) as any;
            console.log("\n💳 Processando Pagamento...");
            await new Promise(r => setTimeout(r, 2000)); 

            const bal = await getBalance(d.client.address, client);
            if (bal < d.price) {
                console.error(`   ❌ Saldo Insuficiente: ${bal}`);
                msg.respond(jc.encode({ ok: false, error: "Saldo Insuficiente" }));
                return;
            }

            try {
                const kp = Ed25519Keypair.fromSecretKey(d.client.secret);
                const tx = new Transaction();
                const [coin] = tx.splitCoins(tx.gas, [tx.pure.u64(d.price)]);
                tx.transferObjects([coin], d.aux_client.address);
                
                const res = await client.signAndExecuteTransaction({ signer: kp, transaction: tx, options: { showEffects: true } });
                
                if (res.effects?.status.status === 'success') {
                    console.log(`   ✅ Pago! Digest: ${res.digest}`);
                    msg.respond(jc.encode({ ok: true, client: d.client }));
                } else {
                    msg.respond(jc.encode({ ok: false }));
                }
            } catch (error) {
                console.error("   ❌ Erro Pagamento:", error);
                msg.respond(jc.encode({ ok: false }));
            }
        },
    });
}

// Mint de cartas on-chain (Move call)
async function handleMintCard(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient, adminKey: Ed25519Keypair) {
    nc.subscribe("internalServer.mintCard", {
        callback(err, msg) {
            if (err) return;
            
            adminQueue = adminQueue.then(async () => {
                const req = jc.decode(msg.data) as any;
                console.log(`📦 Mintando carta ${req.value}...`);
                
                try {
                    const res = await executeWithRetry(client, adminKey, () => {
                        const tx = new Transaction();
                        tx.moveCall({
                            target: `${PACKAGE_ID}::core::mint_card`,
                            arguments: [ tx.object(ADMIN_CAP_ID), tx.pure.u64(req.value), tx.pure.address(req.address) ]
                        });
                        return tx;
                    }, "MintCard");

                    // Busca o ID do objeto criado
                    let createdId = "";
                    if (res.objectChanges) {
                        const created = res.objectChanges.find((o: any) => o.type === 'created');
                        if (created) {
                            createdId = (created as any).objectId;
                        }
                    }

                    console.log(`   ✅ Mint OK: ${createdId} (Digest: ${res.digest})`);
                    msg.respond(jc.encode({ ok: true, digest: res.digest, objectId: createdId }));

                } catch (error: any) {
                    console.error("   ❌ Erro Fatal Mint:", error.message);
                    msg.respond(jc.encode({ ok: false }));
                }
            })
            .then(() => new Promise(r => setTimeout(r, 1000)));
        }
    });
}

// Log de partidas (histórico on-chain)
async function handleLogMatch(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient, adminKey: Ed25519Keypair) {
    nc.subscribe("internalServer.logMatch", {
        callback(err, msg) {
            if (err) return;
            adminQueue = adminQueue.then(async () => {
                const req = jc.decode(msg.data) as any;
                try {
                    const res = await executeWithRetry(client, adminKey, () => {
                        const tx = new Transaction();
                        tx.moveCall({
                            target: `${PACKAGE_ID}::core::log_match`,
                            arguments: [ tx.pure.address(req.winner), tx.pure.address(req.loser), tx.pure.u64(req.val_win), tx.pure.u64(req.val_lose) ]
                        });
                        return tx;
                    }, "LogMatch");

                    let createdId = "";
                    if (res.objectChanges) {
                        const created = res.objectChanges.find((o: any) => o.type === 'created');
                        if (created) {
                            createdId = (created as any).objectId;
                        }
                    }
                    console.log("   ✅ Match Logged.");
                    msg.respond(jc.encode({ ok: true, digest: res.digest, objectId: createdId }));
                } catch (error) {
                    console.error("   ❌ Erro Log:", error);
                    msg.respond(jc.encode({ ok: false }));
                }
            })
            .then(() => new Promise(r => setTimeout(r, 1000)));
        }
    });
}

// Transferência de carta (Move call)
async function handleTransferCard(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient) {
    nc.subscribe("internalServer.transferCard", {
        async callback(err, msg) {
            if (err) return;
            const req = jc.decode(msg.data) as any;
            try {
                const signer = Ed25519Keypair.fromSecretKey(req.ownerSecret);
                const tx = new Transaction();
                tx.moveCall({
                    target: `${PACKAGE_ID}::core::transfer_card`,
                    arguments: [ tx.object(req.cardObjectId), tx.pure.address(req.recipient) ]
                });
                await client.signAndExecuteTransaction({ signer: signer, transaction: tx });
                msg.respond(jc.encode({ ok: true }));
            } catch (error) {
                msg.respond(jc.encode({ ok: false }));
            }
        }
    });
}

// Busca cartas pertencentes ao jogador na blockchain
async function handleGetPlayerCards(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient) {
    nc.subscribe("internalServer.getCards", {
        async callback(err, msg) {
            if (err) return;
            const req = jc.decode(msg.data) as any;
            const address = req.address;

            console.log(`🔍 Buscando cartas na Blockchain para: ${address.substring(0,6)}...`);

            try {
                const structType = `${PACKAGE_ID}::core::MonsterCard`;

                const { data } = await client.getOwnedObjects({
                    owner: address,
                    filter: { StructType: structType },
                    options: { 
                        showContent: true,
                        showType: true 
                    }
                });

                const cards = data.map((obj: any) => {
                    const content = obj.data?.content as any;
                    return {
                        id: obj.data?.objectId,
                        power: parseInt(content?.fields?.value || "0") 
                    };
                });

                console.log(`   ✅ Encontradas ${cards.length} cartas.`);
                msg.respond(jc.encode({ ok: true, cards: cards }));

            } catch (error: any) {
                console.error("   ❌ Erro ao buscar cartas:", error);
                msg.respond(jc.encode({ ok: false, error: error.message }));
            }
        }
    });
}

// Validação de ownership (quem é dono do objeto)
async function handleValidateOwnership(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient) {
    nc.subscribe("internalServer.validateOwnership", {
        async callback(err, msg) {
            if (err) return;
            const req = jc.decode(msg.data) as any;
            
            console.log(`🔍 Validando se ${req.address} possui ${req.objectId}...`);
            const owns = await verifyOwnership(client, req.address, req.objectId);
            
            msg.respond(jc.encode({ ok: owns }));
        }
    });
}

// Troca à Prova de Falhas (com retentativas)
async function handleAtomicSwap(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: IotaClient) {
    nc.subscribe("internalServer.atomicSwap", {
        callback(err, msg) {
            if (err) return;

            adminQueue = adminQueue.then(async () => {
                const req = jc.decode(msg.data) as any;
                console.log(`🤝 Executando Troca: ${req.userA_Addr} <-> ${req.userB_Addr}`);

                try {
                    const signerA = Ed25519Keypair.fromSecretKey(req.userA_Secret);
                    const signerB = Ed25519Keypair.fromSecretKey(req.userB_Secret);

                    await ensureFunds(signerA.toIotaAddress(), client);
                    await ensureFunds(signerB.toIotaAddress(), client);

                    console.log("   ➡️ Passo 1: A envia para B...");
                    const res1 = await executeWithRetry(client, signerA, () => {
                        const tx = new Transaction();
                        tx.moveCall({
                            target: `${PACKAGE_ID}::core::transfer_card`,
                            arguments: [ tx.object(req.cardA_ID), tx.pure.address(req.userB_Addr) ]
                        });
                        return tx;
                    }, "SwapStep1");

                    console.log("   ⬅️ Passo 2: B envia para A...");
                    const res2 = await executeWithRetry(client, signerB, () => {
                        const tx = new Transaction();
                        tx.moveCall({
                            target: `${PACKAGE_ID}::core::transfer_card`,
                            arguments: [ tx.object(req.cardB_ID), tx.pure.address(req.userA_Addr) ]
                        });
                        return tx;
                    }, "SwapStep2");

                    const digest1 = res1.digest;
                    const digest2 = res2.digest;

                    console.log(`   ✅ Troca Finalizada! Digests: ${digest1} | ${digest2}`);
                    msg.respond(jc.encode({ ok: true, digest: digest2 }));

                } catch (error: any) {
                    console.error("   ❌ Erro na Troca:", error.message || error);
                    msg.respond(jc.encode({ ok: false, error: "Falha na Troca (Parcial ou Total)" }));
                }
            });
        }
    });
}

// --- MAIN ---
async function main() {
    if (!PACKAGE_ID || !ADMIN_SECRET) {
        console.error("❌ ERRO: .env incompleto.");
        process.exit(1);
    }
    const nc = await nats.connect({ servers: "localhost:4222" });
    const jc = nats.JSONCodec();
    const client = new IotaClient({ url: NETWORK_URL });
    const adminKey = Ed25519Keypair.fromSecretKey(ADMIN_SECRET);

    console.log(`👑 Admin Iniciado: ${adminKey.toIotaAddress()}`);
    await forceTreasuryRefill(adminKey.toIotaAddress(), client);

    console.log("🚀 Sistema Pronto (Com Leitura On-Chain).");

    // Inicializa todos os handlers
    handleCreateWallet(nc, jc, client, adminKey);
    handleGetBalance(nc, jc, client); 
    handleTransaction(nc, jc, client);
    handleMintCard(nc, jc, client, adminKey);
    handleLogMatch(nc, jc, client, adminKey);
    handleTransferCard(nc, jc, client);
    handleGetPlayerCards(nc, jc, client);
    handleValidateOwnership(nc, jc, client);
    handleAtomicSwap(nc, jc, client);
}

main().catch(console.error);
