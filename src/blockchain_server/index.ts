import { getFullnodeUrl, SuiClient } from '@mysten/sui/client';
import { Ed25519Keypair } from '@mysten/sui/keypairs/ed25519';
import { Transaction } from '@mysten/sui/transactions';
import { requestSuiFromFaucetV0 } from '@mysten/sui/faucet';
import * as nats from "nats";
import * as dotenv from 'dotenv';

dotenv.config();

// --- CONFIGURAÇÕES ---
const NETWORK_URL = process.env.NETWORK_URL || 'http://127.0.0.1:9000';
const FAUCET_URL = process.env.FAUCET_URL || 'http://127.0.0.1:9123/gas';
// Adicionando verificação de segurança para variáveis obrigatórias
const PACKAGE_ID = process.env.PACKAGE_ID!;
const ADMIN_CAP_ID = process.env.ADMIN_CAP_ID!;
const ADMIN_SECRET = process.env.ADMIN_SECRET!;

// --- FUNÇÕES UTILITÁRIAS ---

function createWallet(){
    const keypair = new Ed25519Keypair();
    const secret = keypair.getSecretKey();
    const address = keypair.toSuiAddress(); // MUDANÇA: toSuiAddress

    console.log(`🆕 Carteira criada:`);
    console.log(`   🔑 PrivateKey: ${secret}`);
    console.log(`   📮 Address:    ${address}`);

    return { secret, address };
}

async function faucetWallet(addr: string) {
    console.log('🚰 Solicitando fundos ao Faucet...');
    try {
        await requestSuiFromFaucetV0({ // MUDANÇA: requestSuiFromFaucetV0
            host: FAUCET_URL,
            recipient: addr,
        }); 
        return 1;
    } catch (e) {
        console.error("Erro no Faucet. Verifique se a rede local está rodando com --with-faucet");
        return 0;
    }
}

async function getBalance(addr: string, client: SuiClient) { // MUDANÇA: SuiClient
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

// Garante que a carteira tenha dinheiro para operar
async function ensureFunds(address: string, client: SuiClient) {
    let { data } = await client.getCoins({ owner: address });
    let balance = data.reduce((sum, c) => sum + parseInt(c.balance), 0);
    
    // Se tiver menos de 0.5 Token, recarrega
    if (balance < 500_000_000) {
        console.log(`📉 Saldo baixo (${balance}). Recarregando Faucet para ${address}...`);
        try {
            await requestSuiFromFaucetV0({
                host: process.env.FAUCET_URL!,
                recipient: address
            });
            
            // Espera ativa: Aguarda até 5 segundos pelo saldo
            console.log("⏳ Aguardando recarga...");
            for (let i = 0; i < 5; i++) {
                await new Promise(r => setTimeout(r, 1000)); // Espera 1s
                const newData = await client.getCoins({ owner: address });
                const newBalance = newData.data.reduce((sum, c) => sum + parseInt(c.balance), 0);
                if (newBalance > balance) {
                    console.log("✅ Recarga confirmada!");
                    break;
                }
            }
        } catch (e) {
            console.log("Erro no faucet (talvez rate limit), tentando seguir com o que tem...");
        }
    }
}

async function performTransaction(src: Ed25519Keypair, dest: string, amountToSend: number, client: SuiClient) {
    const tx = new Transaction();

    // Comando: SplitCoins (Tira do Gas) -> TransferObjects (Envia)
    // MUDANÇA: tx.pure.u64 para garantir tipagem correta no Move
    const [coin] = tx.splitCoins(tx.gas, [tx.pure.u64(amountToSend)]);
    tx.transferObjects([coin], dest);

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

// --- HANDLERS DO NATS ---

async function handleCreateWallet(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: SuiClient){ // Adicione client aqui
    nc.subscribe("internalServer.wallet", {
        async callback(err, msg) { // Adicione async aqui
            if (err) return;
            const { secret, address } = createWallet()

            console.log(`⏳ Novo usuário ${address}. Solicitando Faucet e aguardando confirmação...`);
            
            // 1. Pede o dinheiro
            await faucetWallet(address);

            // 2. Trava o processo até o dinheiro aparecer na conta (Polling)
            // Reutiliza a função getBalance que já tem um loop de espera
            const saldoInicial = await getBalance(address, client);

            if (saldoInicial > 0) {
                console.log(`✅ Conta pronta! Saldo confirmado: ${saldoInicial}`);
            } else {
                console.warn("⚠️ Conta criada, mas o saldo ainda não confirmou. Pode falhar na primeira compra.");
            }
            
            const resposta = { ok: true, client: {secret, address} };
            msg.respond(jc.encode(resposta));
        },
    });
}

async function handleGetBalance(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: SuiClient) {
    nc.subscribe("internalServer.balance", {
        async callback(err, msg) {
            if (err) return;
            const decMes = jc.decode(msg.data) as any;
            
            const iotaBalance = await getBalance(decMes.client.address, client)
            const resposta = { ok: true, client: decMes.client, price: iotaBalance };
            msg.respond(jc.encode(resposta));
        },
    });
}

async function handleFaucetWallet(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: SuiClient){
    nc.subscribe("internalServer.faucet", {
        async callback(err, msg) {
            if (err) return;
            const decMes = jc.decode(msg.data) as any;
            
            await faucetWallet(decMes.client.address);
            const iotaBalance = await getBalance(decMes.client.address, client)
            const resposta = { ok: true, client: decMes.client, price: iotaBalance };
            msg.respond(jc.encode(resposta));
        },
    });
}


async function handleTransaction(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: SuiClient){
    nc.subscribe("internalServer.transaction", {
        async callback(err, msg) {
            if (err) return;
            const decMes = jc.decode(msg.data) as any;
            console.log("Recebi Transação de Pagamento");
            
            try {
                // Recupera a chave a partir do segredo
                const keypair = Ed25519Keypair.fromSecretKey(decMes.client.secret);
                const address = keypair.toSuiAddress();

                await ensureFunds(address, client);

                const result = await performTransaction(keypair, decMes.aux_client.address, decMes.price, client)
                
                const resposta = { ok: result.effects?.status.status === 'success', client: decMes.client, };
                msg.respond(jc.encode(resposta));
            } catch (error) {
                console.error("Erro na transação:", error);
                msg.respond(jc.encode({ ok: false }));
            }
        },
    });
}

// 1. MINT (Abrir Pacote)
async function handleMintCard(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: SuiClient, serverKeypair: Ed25519Keypair) {
    nc.subscribe("internalServer.mintCard", {
        async callback(err, msg) {
            if (err) return;
            const req = jc.decode(msg.data) as any;
            console.log(`📦 Mintando carta valor ${req.value} para ${req.address}`);

            try {
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
            } catch (error) {
                console.error("Erro no Mint:", error);
                msg.respond(jc.encode({ ok: false, error: "Mint failed" }));
            }
        }
    });
}

// 2. LOG MATCH (Salvar Resultado)
async function handleLogMatch(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: SuiClient, serverKeypair: Ed25519Keypair) {
    nc.subscribe("internalServer.logMatch", {
        async callback(err, msg) {
            if (err) return;
            const req = jc.decode(msg.data) as any;
            
            try {
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

                await client.signAndExecuteTransaction({ signer: serverKeypair, transaction: tx });
                msg.respond(jc.encode({ ok: true }));
            } catch (error) {
                console.error("Erro no LogMatch:", error);
                msg.respond(jc.encode({ ok: false }));
            }
        }
    });
}

// 3. TRANSFERÊNCIA DE CARTA (Troca)
async function handleTransferCard(nc: nats.NatsConnection, jc: nats.Codec<unknown>, client: SuiClient) {
    nc.subscribe("internalServer.transferCard", {
        async callback(err, msg) {
            if (err) return;
            const req = jc.decode(msg.data) as any;
            
            try {
                // Recupera a carteira do dono da carta
                const signer = Ed25519Keypair.fromSecretKey(req.ownerSecret);
                
                const tx = new Transaction();
                tx.moveCall({
                    target: `${PACKAGE_ID}::core::transfer_card`,
                    arguments: [
                        tx.object(req.cardObjectId),
                        tx.pure.address(req.recipient)
                    ]
                });

                await client.signAndExecuteTransaction({ signer: signer, transaction: tx });
                msg.respond(jc.encode({ ok: true }));
            } catch (error) {
                console.error("Erro Transferencia:", error);
                msg.respond(jc.encode({ ok: false }));
            }
        }
    });
}

async function main() {
    // 1. Validação de Segurança
    if (!PACKAGE_ID || !ADMIN_SECRET) {
        console.error("❌ ERRO: Variáveis de ambiente não encontradas.");
        console.error("   Rode 'npm run deploy' primeiro!");
        process.exit(1);
    }

    // 2. Conexões de Infraestrutura
    const nc = await nats.connect({ servers: "localhost:4222" });
    const jc = nats.JSONCodec();
    
    const client = new SuiClient({ url: NETWORK_URL }); // MUDANÇA: SuiClient
    console.log(`📡 Conectado à rede em: ${NETWORK_URL}`);

    // 3. Carregar Chave do Servidor
    const serverKeypair = Ed25519Keypair.fromSecretKey(ADMIN_SECRET);
    console.log(`👑 Admin carregado: ${serverKeypair.toSuiAddress()}`);

    console.log("🚀 Iniciando Workers...");

    // 4. Iniciar Handlers
    handleCreateWallet(nc, jc, client);
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