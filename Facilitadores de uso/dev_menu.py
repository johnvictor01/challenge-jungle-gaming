#!/usr/bin/env python3
"""Menu interativo local para desenvolver e depurar a API do challenge."""

from __future__ import annotations

import json
import os
import signal
import subprocess
import sys
import time
from pathlib import Path
from urllib.error import HTTPError, URLError
from urllib.parse import urlencode
from urllib.request import Request, urlopen


ROOT = Path(__file__).resolve().parents[1]
ENV_FILE = ROOT / ".env"
ENV_EXAMPLE = ROOT / ".env.example"
LOCAL_DIR = ROOT / ".local"


def load_env_file() -> dict[str, str]:
    path = ENV_FILE if ENV_FILE.exists() else ENV_EXAMPLE
    if not path.exists():
        raise RuntimeError("Não encontrei .env nem .env.example na raiz do projeto.")
    values: dict[str, str] = {}
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
        values[key.strip()] = value
    return values


def endpoint_url(env: dict[str, str]) -> str:
    address = env.get("HTTP_ADDR", ":8080").strip()
    if address.startswith(":"):
        address = "localhost" + address
    if "://" not in address:
        address = "http://" + address
    return address.rstrip("/")


def show_json(value: object) -> None:
    print(json.dumps(value, ensure_ascii=False, indent=2))


class DevMenu:
    def __init__(self, env: dict[str, str]) -> None:
        self.env = env
        self.base_url = endpoint_url(env)
        self.api_process: subprocess.Popen[bytes] | None = None
        self.wallet_id = ""
        self.player_id = ""
        self.last_wager: tuple[dict[str, object], str] | None = None

    def compose(self, compose_file: str, *args: str) -> list[str]:
        command = ["docker", "compose"]
        env_file = ENV_FILE if ENV_FILE.exists() else ENV_EXAMPLE
        if env_file.exists():
            command.extend(["--env-file", str(env_file)])
        command.extend(["-f", compose_file, *args])
        return command

    def run(self, command: list[str]) -> bool:
        print("\n$ " + " ".join(command))
        try:
            result = subprocess.run(command, cwd=ROOT, env={**os.environ, **self.env})
        except FileNotFoundError as exc:
            print(f"Comando não encontrado: {exc.filename}")
            return False
        if result.returncode != 0:
            print(f"O comando terminou com código {result.returncode}.")
            return False
        return True

    def start_dependencies(self) -> None:
        steps = [
            self.compose("deploy/postgres.compose.yaml", "up", "-d", "postgres"),
            self.compose("deploy/postgres.compose.yaml", "--profile", "tools", "run", "--rm", "migrate", "up"),
            self.compose("deploy/keycloak.compose.yaml", "up", "-d"),
            self.compose("deploy/sqs.compose.yaml", "up", "-d"),
        ]
        for command in steps:
            if not self.run(command):
                print("Parei aqui para você poder corrigir o erro antes de continuar.")
                return
        print("Dependências e migrations prontas.")
        self.start_api()

    def start_api(self) -> None:
        if self.api_process and self.api_process.poll() is None:
            print("A API iniciada por este menu já está rodando.")
            return
        if self.health(check_only=True):
            print(f"Já existe uma API respondendo em {self.base_url}.")
            return
        LOCAL_DIR.mkdir(exist_ok=True)
        log_path = LOCAL_DIR / "dev-menu-api.log"
        log_file = log_path.open("ab")
        self.api_process = subprocess.Popen(
            ["go", "run", "./cmd/api"],
            cwd=ROOT,
            env={**os.environ, **self.env},
            stdout=log_file,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
        log_file.close()
        print(f"Iniciei a API. Log local: {log_path.relative_to(ROOT)}")
        for _ in range(40):
            if self.api_process.poll() is not None:
                print("A API encerrou durante a inicialização. Confira o log acima.")
                return
            if self.health(check_only=True):
                print(f"API pronta em {self.base_url}.")
                return
            time.sleep(0.5)
        print("A API ainda não respondeu. Confira o log local e tente a opção de health.")

    def stop_api(self) -> None:
        if not self.api_process or self.api_process.poll() is not None:
            print("Este menu não iniciou uma API ativa.")
            return
        os.killpg(self.api_process.pid, signal.SIGTERM)
        try:
            self.api_process.wait(timeout=12)
        except subprocess.TimeoutExpired:
            os.killpg(self.api_process.pid, signal.SIGKILL)
            self.api_process.wait()
        print("API parada.")

    def stop_dependencies(self) -> None:
        for compose_file, service in [
            ("deploy/sqs.compose.yaml", "localstack"),
            ("deploy/keycloak.compose.yaml", "keycloak"),
            ("deploy/postgres.compose.yaml", "postgres"),
        ]:
            self.run(self.compose(compose_file, "stop", service))
        print("Serviços parados; os volumes e os dados foram preservados.")

    def token(self, client_id: str, secret_name: str) -> str:
        issuer = self.env.get("OIDC_ISSUER_URL", "").rstrip("/")
        secret = self.env.get(secret_name, "")
        if not issuer or not secret:
            raise RuntimeError(f"Configure OIDC_ISSUER_URL e {secret_name} no .env.")
        request = Request(
            f"{issuer}/protocol/openid-connect/token",
            data=urlencode({
                "grant_type": "client_credentials",
                "client_id": client_id,
                "client_secret": secret,
            }).encode(),
            headers={"Content-Type": "application/x-www-form-urlencoded"},
            method="POST",
        )
        result = self.http(request)
        token = result.get("access_token")
        if not isinstance(token, str) or not token:
            raise RuntimeError(f"Keycloak não retornou token: {result}")
        return token

    @staticmethod
    def http(request: Request) -> dict[str, object]:
        try:
            with urlopen(request, timeout=20) as response:
                body = response.read().decode("utf-8")
        except HTTPError as exc:
            body = exc.read().decode("utf-8", errors="replace")
            try:
                parsed = json.loads(body)
            except json.JSONDecodeError:
                parsed = {"error": body or str(exc)}
            show_json({"httpStatus": exc.code, "response": parsed})
            raise RuntimeError("A API recusou a chamada.") from exc
        except URLError as exc:
            raise RuntimeError(f"Não consegui acessar {request.full_url}: {exc.reason}") from exc
        try:
            parsed = json.loads(body)
        except json.JSONDecodeError as exc:
            raise RuntimeError(f"A resposta não veio em JSON: {body}") from exc
        if not isinstance(parsed, dict):
            raise RuntimeError(f"Resposta inesperada: {parsed}")
        return parsed

    def api_request(self, method: str, path: str, token: str, body: dict[str, object] | None = None, extra_headers: dict[str, str] | None = None) -> dict[str, object]:
        data = json.dumps(body).encode() if body is not None else None
        headers = {"Authorization": f"Bearer {token}", "Content-Type": "application/json"}
        if extra_headers:
            headers.update(extra_headers)
        request = Request(f"{self.base_url}{path}", data=data, headers=headers, method=method)
        return self.http(request)

    def health(self, check_only: bool = False) -> bool:
        try:
            live = self.http(Request(f"{self.base_url}/health/live", method="GET"))
            if check_only:
                return live.get("status") == "live"
            ready = self.http(Request(f"{self.base_url}/health/ready", method="GET"))
            metrics = urlopen(f"{self.base_url}/metrics", timeout=5).read().decode()
            print("Liveness:")
            show_json(live)
            print("Readiness:")
            show_json(ready)
            print(f"Métricas disponíveis ({len(metrics.splitlines())} linhas).")
            return ready.get("status") == "ready"
        except (RuntimeError, URLError, TimeoutError) as exc:
            if not check_only:
                print(exc)
            return False

    def create_wallet(self) -> None:
        player_default = f"menu-player-{int(time.time())}"
        player = input(f"Player ID [{player_default}]: ").strip() or player_default
        amount = input("Saldo inicial [100.00]: ").strip() or "100.00"
        currency = input("Moeda [BRL]: ").strip().upper() or "BRL"
        token = self.token("internal-service", "TEST_INTERNAL_CLIENT_SECRET")
        response = self.api_request("POST", "/wallets", token, {
            "playerId": player,
            "initialBalance": {"amount": amount, "currency": currency},
        })
        self.wallet_id = str(response.get("id", ""))
        self.player_id = player
        print("Carteira criada:")
        show_json(response)

    def wager(self) -> None:
        if not self.health(check_only=True):
            raise RuntimeError("A API não está respondendo. Use a opção de iniciar a API.")
        player = input(f"Player ID [{self.player_id or 'menu-player'}]: ").strip() or self.player_id or "menu-player"
        wallet = input(f"Wallet ID [{self.wallet_id or 'informe o ID'}]: ").strip() or self.wallet_id
        if not wallet:
            raise RuntimeError("Informe um Wallet ID ou crie uma carteira pelo menu.")
        kind = input("Tipo BET/WIN/LOSS/REFUND/ROLLBACK [BET]: ").strip().upper() or "BET"
        if kind not in {"BET", "WIN", "LOSS", "REFUND", "ROLLBACK"}:
            raise RuntimeError("Tipo de operação inválido.")
        amount_default = "0.00" if kind == "LOSS" else "10.00"
        amount = input(f"Valor [{amount_default}]: ").strip() or amount_default
        currency = input("Moeda [BRL]: ").strip().upper() or "BRL"
        external_default = f"menu-{int(time.time())}"
        external_id = input(f"External transaction ID [{external_default}]: ").strip() or external_default
        reference = ""
        if kind in {"REFUND", "ROLLBACK", "WIN"}:
            reference = input("Referência externa (pode deixar vazia para WIN): ").strip()
        body: dict[str, object] = {
            "providerId": "provider-a",
            "externalTransactionId": external_id,
            "playerId": player,
            "walletId": wallet,
            "roundId": input("Round ID [menu-round]: ").strip() or "menu-round",
            "gameId": input("Game ID [menu-game]: ").strip() or "menu-game",
            "kind": kind,
            "money": {"amount": amount, "currency": currency},
        }
        if reference:
            body["referenceExternalTransactionId"] = reference
        idem_key = input(f"Idempotency-Key [provider-a:{external_id}]: ").strip() or f"provider-a:{external_id}"
        token = self.token("provider-a", "TEST_PROVIDER_CLIENT_SECRET")
        response = self.api_request("POST", "/wagering/transactions", token, body, {"Idempotency-Key": idem_key})
        self.last_wager = (body, idem_key)
        print("Resultado da operação:")
        show_json(response)

    def replay_last_wager(self) -> None:
        if not self.last_wager:
            print("Ainda não enviei uma operação nesta sessão do menu.")
            return
        body, idem_key = self.last_wager
        token = self.token("provider-a", "TEST_PROVIDER_CLIENT_SECRET")
        response = self.api_request("POST", "/wagering/transactions", token, body, {"Idempotency-Key": idem_key})
        print("Reenvio da mesma operação e da mesma chave:")
        show_json(response)

    def wallet_action(self, action: str) -> None:
        wallet = input(f"Wallet ID [{self.wallet_id or 'informe o ID'}]: ").strip() or self.wallet_id
        if not wallet:
            raise RuntimeError("Informe um Wallet ID ou crie uma carteira pelo menu.")
        token = self.token("internal-service", "TEST_INTERNAL_CLIENT_SECRET")
        if action == "read":
            result = self.api_request("GET", f"/wallets/{wallet}", token)
        elif action == "ledger":
            result = self.api_request("GET", f"/wallets/{wallet}/ledger?limit=50", token)
        else:
            result = self.api_request("POST", f"/wallets/{wallet}/reconciliation", token)
        show_json(result)

    def run_tests(self) -> None:
        print("Os testes de integração usam PostgreSQL, Keycloak e LocalStack locais.")
        if input("Executar go test -race ./...? [s/N]: ").strip().lower() == "s":
            self.run(["go", "test", "-race", "./...", "-count=1"])
        if input("Executar go vet ./...? [s/N]: ").strip().lower() == "s":
            self.run(["go", "vet", "./..."])

    def stop(self) -> None:
        self.stop_api()
        self.stop_dependencies()

    def run_menu(self) -> None:
        actions = {
            "1": ("Iniciar dependências, migrations e API", self.start_dependencies),
            "2": ("Iniciar API", self.start_api),
            "3": ("Criar carteira", self.create_wallet),
            "4": ("Enviar operação BET/WIN/LOSS/REFUND/ROLLBACK", self.wager),
            "5": ("Repetir última operação (testar idempotência)", self.replay_last_wager),
            "6": ("Consultar carteira", lambda: self.wallet_action("read")),
            "7": ("Consultar ledger", lambda: self.wallet_action("ledger")),
            "8": ("Reconciliar saldo pelo ledger", lambda: self.wallet_action("reconcile")),
            "9": ("Ver health e métricas", self.health),
            "10": ("Rodar testes e go vet", self.run_tests),
            "11": ("Parar API e serviços (preserva volumes)", self.stop),
        }
        try:
            while True:
                print("\n=== Menu local do backend challenge ===")
                for key, (label, _) in actions.items():
                    print(f"{key}. {label}")
                print("0. Sair")
                choice = input("Escolha: ").strip()
                if choice == "0":
                    break
                selected = actions.get(choice)
                if not selected:
                    print("Opção inválida.")
                    continue
                try:
                    selected[1]()
                except (RuntimeError, ValueError) as exc:
                    print(f"Erro: {exc}")
                input("\nPressione Enter para voltar ao menu...")
        except (KeyboardInterrupt, EOFError):
            print("\nSaindo do menu.")
        finally:
            if self.api_process:
                self.stop_api()


def main() -> int:
    try:
        env = load_env_file()
        DevMenu(env).run_menu()
    except RuntimeError as exc:
        print(f"Erro: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
