const mongoHost = process.env.SPECIALIST_BOOKING_API_MONGODB_HOST;
const mongoPort = process.env.SPECIALIST_BOOKING_API_MONGODB_PORT;
const mongoUser = process.env.SPECIALIST_BOOKING_API_MONGODB_USERNAME;
const mongoPassword = process.env.SPECIALIST_BOOKING_API_MONGODB_PASSWORD;
const database = process.env.SPECIALIST_BOOKING_API_MONGODB_DATABASE;
const collection = process.env.SPECIALIST_BOOKING_API_MONGODB_COLLECTION;
const retrySeconds = parseInt(process.env.RETRY_CONNECTION_SECONDS || "5") || 5;

let connection;
while (true) {
  try {
    connection = Mongo(`mongodb://${mongoUser}:${mongoPassword}@${mongoHost}:${mongoPort}`);
    break;
  } catch (exception) {
    print(`Cannot connect to mongoDB: ${exception}`);
    print(`Will retry after ${retrySeconds} seconds`);
    sleep(retrySeconds * 1000);
  }
}

const databases = connection.getDBNames();
if (databases.includes(database)) {
  const dbInstance = connection.getDB(database);
  collections = dbInstance.getCollectionNames();
  if (collections.includes(collection)) {
    print(`Collection '${collection}' already exists in database '${database}'`);
    process.exit(0);
  }
}

const db = connection.getDB(database);
db.createCollection(collection);
db[collection].createIndex({ id: 1 });

let now = new Date();
now.setDate(now.getDate() + 1);
let result = db[collection].insertMany([
  {
    id: "specialist-clinic",
    appointments: [
      {
        id: "apt-001",
        patientId: "p-10001",
        patientName: "Jana Nováková",
        patientEmail: "jana@example.com",
        referringDoctor: "MUDr. Kováč",
        startsAt: now,
        durationMinutes: 30,
        examinationType: "Kardiologické vyšetrenie",
        status: "confirmed",
        note: "Kontrola EKG"
      }
    ],
    timeSlots: [
      {
        id: "slot-001",
        startsAt: now,
        durationMinutes: 30,
        capacity: 2,
        booked: 1,
        examinationType: "Kardiologické vyšetrenie",
        urgentBlocked: false
      }
    ]
  }
]);

if (result.writeError) {
  console.error(result);
  print(`Error when writing the data: ${result.errmsg}`);
}
process.exit(0);
