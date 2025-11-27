import { getFullnodeUrl, IotaClient } from '@iota/iota-sdk/client';
import { Ed25519Keypair } from '@iota/iota-sdk/keypairs/ed25519';
//import { Transaction } from '@iota/iota-sdk/transactions';
import { requestIotaFromFaucetV0 } from '@iota/iota-sdk/faucet';
import * as nats from "nats";

// --- CONFIGURAÇÕES ---
// URL da rede local (padrão do 'iota start')
const NETWORK_URL = 'http://127.0.0.1:9000';
// URL do Faucet local
const FAUCET_URL = 'http://127.0.0.1:9123/gas';


function createWallet(){
    const keypair = new Ed25519Keypair();
    const address = keypair.getPublicKey().toIotaAddress();
    console.log(`👤 Carteira criada (Remetente): ${address}`);
    return address
}

async function main() {


    const nc = await nats.connect({ servers: "localhost:4222" });
    const jc = nats.JSONCodec();
    const walletRequest = nc.subscribe("internalServer.wallet");

    const client = new IotaClient({ url: NETWORK_URL });
    console.log(`📡 Conectado à rede em: ${NETWORK_URL}`);

    for await (const msg of walletRequest) {
        const decMess = jc.decode(msg.data);
        console.log("Recebi:", decMess);
        
        const resposta = { ok: false, msg: createWallet() };
        msg.respond(jc.encode(resposta));
    }

    // console.log('--- Iniciando Demo IOTA (SDK TypeScript) ---\n');

    // // 1. Conectar ao Cliente IOTA
    // const client = new IotaClient({ url: NETWORK_URL });
    // console.log(`📡 Conectado à rede em: ${NETWORK_URL}`);

    // // 2. Criar uma Carteira (Remetente)
    // // Gera um par de chaves Ed25519 novo
    // const keypair = new Ed25519Keypair();
    // const address = keypair.getPublicKey().toIotaAddress();
    // console.log(`👤 Carteira criada (Remetente): ${address}`);

    // // 3. Solicitar Fundos ao Faucet
    // console.log('🚰 Solicitando fundos ao Faucet...');
    // try {
    //     await requestIotaFromFaucetV0({
    //         host: FAUCET_URL,
    //         recipient: address,
    //     });
    // } catch (e) {
    //     console.error("Erro no Faucet. Verifique se a rede local está rodando com --with-faucet");
    //     return;
    // }

    // // Aguardar um pouco para a rede processar o saldo (Polling simples)
    // console.log('⏳ Aguardando confirmação do saldo...');
    // let balance = 0;
    // while (balance <= 0) {
    //     const balanceData = await client.getCoins({ owner: address });
    //     if (balanceData.data.length > 0) {
    //         balance = parseInt(balanceData.data[0].balance);
    //     } else {
    //         await new Promise(r => setTimeout(r, 1000)); // Espera 1s
    //     }
    // }
    // console.log(`💰 Saldo recebido: ${balance} NANOS`);

    // // 4. Criar um Destinatário (apenas para receber)
    // const recipientKeypair = new Ed25519Keypair();
    // const recipientAddress = recipientKeypair.getPublicKey().toIotaAddress();
    // console.log(`🎯 Endereço de Destino: ${recipientAddress}`);

    // // 5. Construir a Transação (Programmable Transaction Block)
    // const tx = new Transaction();

    // // Lógica:
    // // O SDK gerencia o Gas automaticamente (Coin Selection).
    // // Vamos dividir uma moeda de Gas para criar o valor que queremos enviar.
    // const amountToSend = 1000; // 1000 NANOS
    
    // // Comando: SplitCoins (Tira do Gas) -> TransferObjects (Envia)
    // const [coin] = tx.splitCoins(tx.gas, [amountToSend]);
    // tx.transferObjects([coin], recipientAddress);

    // // 6. Assinar e Executar
    // console.log('\n🚀 Enviando transação...');
    // const result = await client.signAndExecuteTransaction({
    //     signer: keypair,
    //     transaction: tx,
    //     options: {
    //         showEffects: true,
    //         showBalanceChanges: true,
    //     },
    // });

    // // 7. Resultados
    // console.log(`✅ Transação Confirmada! Digest: ${result.digest}`);
    
    // if (result.effects?.status.status === 'success') {
    //     console.log('🎉 Status: SUCESSO');
        
    //     // Mostrar mudança de saldo
    //     result.balanceChanges?.forEach(change => {
    //         const quem = change.owner === ((address as any).AddressOwner || address) ? 'Remetente' : 'Destinatário';
    //         console.log(`   ${quem} (${change.coinType}): ${change.amount} NANOS`);
    //     });
    // } else {
    //     console.error('❌ Falha na transação:', result.effects?.status.error);
    // }
}

main().catch(console.error);