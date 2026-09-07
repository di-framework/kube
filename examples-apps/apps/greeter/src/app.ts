import { useContainer } from "@di-framework/core/container";
import { Container } from "@di-framework/core/decorators";
import { TypedRouter } from "@di-framework/http";

@Container()
class GreetingService {
  greet(name: string, language: string) {
    const greetings: Record<string, string> = { en: "Hello", es: "Hola", fr: "Bonjour" };
    if (!Object.hasOwn(greetings, language)) return undefined;
    return `${greetings[language]}, ${name}!`;
  }
}

const greetings = useContainer().resolve(GreetingService);
const router = TypedRouter();
router.get("/", () => Response.json({ app: "greeter", routes: ["/health", "/greet/:name?lang=en"] }));
router.get("/health", () => Response.json({ app: "greeter", status: "ok" }));
router.get("/greet/:name", (request) => {
  const language = new URL(request.url).searchParams.get("lang") ?? "en";
  const message = greetings.greet(request.params?.name ?? "world", language);
  return message
    ? Response.json({ message, language, platform: "di-framework-kube" })
    : Response.json({ error: "Supported languages: en, es, fr" }, { status: 400 });
});
router.all("*", () => Response.json({ error: "Not found" }, { status: 404 }));

export default router;
