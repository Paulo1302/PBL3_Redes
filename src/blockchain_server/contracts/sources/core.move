module game_assets::core {
    use std::string::{Self, String};
    use iota::event;
    use iota::transfer;
    use iota::object;
    use iota::tx_context::{Self, TxContext};

    //
    // STRUCTS
    //

    public struct AdminCap has key, store {
        id: UID,
    }

    public struct MonsterCard has key, store {
        id: UID,
        value: u64,
    }

    public struct MatchLog has key {
        id: UID,
        winner: address,
        loser: address,
        card_winner: u64,
        card_loser: u64,
    }

    //
    // INIT — IOTA Move: NÃO É ENTRY e NÃO É PUBLIC!
    //
    fun init(ctx: &mut TxContext) {
        // Cria o admin cap
        let cap = AdminCap { id: object::new(ctx) };

        // Entrega para o address que publicou o package
        let publisher = tx_context::sender(ctx);
        transfer::transfer(cap, publisher);
    }

    //
    // FUNÇÕES DE NEGÓCIO
    //

    public entry fun mint_card(
        _cap: &AdminCap,
        value: u64,
        recipient: address,
        ctx: &mut TxContext
    ) {
        let card = MonsterCard {
            id: object::new(ctx),
            value,
        };

        transfer::public_transfer(card, recipient);
    }

    public entry fun log_match(
        winner: address,
        loser: address,
        val_win: u64,
        val_lose: u64,
        ctx: &mut TxContext
    ) {
        let log = MatchLog {
            id: object::new(ctx),
            winner,
            loser,
            card_winner: val_win,
            card_loser: val_lose,
        };

        transfer::freeze_object(log);
    }

    public entry fun transfer_card(card: MonsterCard, recipient: address) {
        transfer::public_transfer(card, recipient);
    }
}
