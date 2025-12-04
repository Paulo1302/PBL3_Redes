module game_assets::core {
    use std::string::{Self, String};
    use iota::event;

    // --- STRUCTS ---

    // A Carta é um NFT (key, store)
    public struct MonsterCard has key, store {
        id: UID,
        value: u64, // O valor numérico para o jogo "Maior vence"
    }

    // Registro de Partida (Imutável)
    public struct MatchLog has key {
        id: UID,
        winner: address,
        loser: address,
        card_winner: u64,
        card_loser: u64,
    }

    // Permissão do Servidor
    public struct AdminCap has key, store { id: UID }

    fun init(ctx: &mut TxContext) {
        transfer::transfer(AdminCap { id: object::new(ctx) }, ctx.sender());
    }

    // --- FUNÇÕES ---

    // 1. Criar Carta (MINT) - Chamado quando abre pacote
    public entry fun mint_card(
        _: &AdminCap, 
        value: u64, 
        recipient: address, 
        ctx: &mut TxContext
    ) {
        let card = MonsterCard {
            id: object::new(ctx),
            value: value
        };
        transfer::public_transfer(card, recipient);
    }

    // 2. Registrar Partida - Chamado ao fim do jogo
    public entry fun log_match(
        winner: address,
        loser: address,
        val_win: u64,
        val_lose: u64,
        ctx: &mut TxContext
    ) {
        let log = MatchLog {
            id: object::new(ctx),
            winner: winner,
            loser: loser,
            card_winner: val_win,
            card_loser: val_lose
        };
        // Congela o objeto para ser um registro histórico imutável
        transfer::freeze_object(log);
    }

    // 3. Transferir (Troca) - O servidor vai orquestrar, mas o dono assina
    public entry fun transfer_card(card: MonsterCard, recipient: address) {
        transfer::public_transfer(card, recipient);
    }
}