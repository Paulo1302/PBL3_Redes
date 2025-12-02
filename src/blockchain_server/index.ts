import { getFullnodeUrl, IotaClient } from '@iota/iota-sdk/client';
import { Ed25519Keypair } from '@iota/iota-sdk/keypairs/ed25519';
import { Transaction } from '@iota/iota-sdk/transactions';
import { requestIotaFromFaucetV0 } from '@iota/iota-sdk/faucet';
import * as nats from "nats";

// --- CONFIGURAÇÕES ---
// URL da rede local (padrão do 'iota start')
const NETWORK_URL = 'http://127.0.0.1:9000';
// URL do Faucet local
const FAUCET_URL = 'http://127.0.0.1:9123/gas';


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
            console.log("Recebi:", decMes);
            
            const keypair = Ed25519Keypair.fromSecretKey(decMes.client.secret);
            const result = await performTransaction(keypair, decMes.aux_client.address, decMes.price, client)
            
            const resposta = { ok: result.effects?.status.status === 'success', client: decMes.client, };
            msg.respond(jc.encode(resposta));
        },
    });
    
}

async function main() {

    const nc = await nats.connect({ servers: "localhost:4222" });
    const jc = nats.JSONCodec();
    

    const client = new IotaClient({ url: NETWORK_URL });
    console.log(`📡 Conectado à rede em: ${NETWORK_URL}`);

    handleCreateWallet(nc, jc)
    handleFaucetWallet(nc, jc, client)
    handleGetBalance(nc, jc, client)
    handleTransaction(nc, jc, client)
}

main().catch(console.error);