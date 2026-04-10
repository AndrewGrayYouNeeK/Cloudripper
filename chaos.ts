import * as http from "http";

interface ChaosConfig {
  provider: "chaos-mesh" | "gremlin";
  endpoint: string;
  apiKey?: string;
}

class ChaosOrchestrator {
  private config: ChaosConfig;

  constructor(config: ChaosConfig) {
    this.config = config;
  }

  /**
   * Inject chaos using Chaos Mesh
   */
  async injectChaosMesh(experiment: any): Promise<any> {
    return new Promise((resolve, reject) => {
      const postData = JSON.stringify(experiment);

      const options = {
        hostname: this.config.endpoint,
        port: 80,
        path: "/api/v1/experiments",
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Content-Length": Buffer.byteLength(postData),
        },
      };

      const req = http.request(options, (res) => {
        let data = "";
        res.on("data", (chunk) => {
          data += chunk;
        });
        res.on("end", () => {
          resolve(JSON.parse(data));
        });
      });

      req.on("error", reject);
      req.write(postData);
      req.end();
    });
  }

  /**
   * Inject chaos using Gremlin
   */
  async injectGremlin(attack: any): Promise<any> {
    return new Promise((resolve, reject) => {
      const postData = JSON.stringify(attack);

      const options = {
        hostname: this.config.endpoint,
        port: 443,
        path: "/v1/attacks",
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          "Authorization": `Bearer ${this.config.apiKey}`,
          "Content-Length": Buffer.byteLength(postData),
        },
      };

      const req = http.request(options, (res) => {
        let data = "";
        res.on("data", (chunk) => {
          data += chunk;
        });
        res.on("end", () => {
          resolve(JSON.parse(data));
        });
      });

      req.on("error", reject);
      req.write(postData);
      req.end();
    });
  }

  /**
   * Generic chaos injection
   */
  async inject(experiment: any): Promise<any> {
    if (this.config.provider === "chaos-mesh") {
      return this.injectChaosMesh(experiment);
    } else if (this.config.provider === "gremlin") {
      return this.injectGremlin(experiment);
    } else {
      throw new Error(`Unknown chaos provider: ${this.config.provider}`);
    }
  }
}

export default ChaosOrchestrator;